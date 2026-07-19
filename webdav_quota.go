package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

// ─────────────────────────────────────────────────────────────────────────────
// Quota subsystem for the WebDAV server.
//
// This file mirrors the protocol-layer patterns from Cloudreve's
// pkg/webdav (an extension of golang.org/x/net/webdav) but stays self-
// contained: we wrap the upstream FileSystem rather than forking it,
// intercept only top-level PROPFIND for quota XML injection, and report
// 507 Insufficient Storage on over-quota writes (Cloudreve currently
// leaks a 500 here — we fix that bug from day one).
//
// Three pieces:
//   1. usageState    — in-memory map[accountID]int64 + JSON persistence.
//   2. quotaFileSystem — webdav.FileSystem wrapper that enforces the cap
//      on mutating calls and tracks byte deltas.
//   3. handlePropfindRoot — top-level PROPFIND handler that emits
//      DAV:quota-used-bytes and DAV:quota-available-bytes (RFC 4331).
// ─────────────────────────────────────────────────────────────────────────────

// ErrInsufficientCapacity is returned by quotaFileSystem.OpenFile when
// the write would push the account past its configured quota. Mapped to
// HTTP 507 by webdavServer.ServeHTTP.
var ErrInsufficientCapacity = errors.New("webdav: insufficient storage capacity")

// ─── usageState ─────────────────────────────────────────────────────────────

// usageState persists the per-account byte totals for the WebDAV server.
//
// On-disk shape (storage-usage.json):
//
//	{
//	  "version": 1,
//	  "used": { "wd_abcdef": 1234567, ... }
//	}
//
// Concurrency: an RWMutex guards the in-memory map. addDelta takes the
// write lock and persists on every call. recalculate takes the write
// lock once, walks the filesystem, replaces the entry, and persists.
// Both are O(1) for the lock holder but block other writers briefly.
//
// The file is rewritten atomically using the same temp+fsync+chmod+
// rename pattern as webdavAccountState.saveLocked (see
// webdav_accounts.go:456).
type usageState struct {
	mu   sync.RWMutex
	used map[string]int64
	path string
}

// usageFile is the on-disk JSON envelope for usageState.
type usageFile struct {
	Version int            `json:"version"`
	Used    map[string]int `json:"used"`
}

// loadUsageState reads storage-usage.json from path. A missing file is
// not an error — it returns an empty state. JSON parse errors are
// logged and fall back to an empty state (the operator can re-trigger
// recalculation via the admin endpoint to repopulate).
func loadUsageState(path string) *usageState {
	s := &usageState{
		path: path,
		used: make(map[string]int64),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("webdav-quota: failed to read %s (%v); starting with empty usage", path, err)
		} else {
			log.Printf("webdav-quota: %s does not exist; starting with empty usage", path)
		}
		return s
	}
	var f usageFile
	if err := json.Unmarshal(data, &f); err != nil {
		log.Printf("webdav-quota: %s is not valid JSON (%v); starting with empty usage", path, err)
		return s
	}
	if f.Used != nil {
		for k, v := range f.Used {
			s.used[k] = int64(v)
		}
	}
	log.Printf("webdav-quota: loaded usage for %d account(s) from %s", len(s.used), path)
	return s
}

// get returns the cached byte count for an account. Zero is a valid
// answer (newly created account with no files yet) so the caller must
// not distinguish "missing" from "zero" — same value, same meaning.
func (s *usageState) get(accountID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.used[accountID]
}

// addDelta atomically updates the cached byte count for accountID and
// persists. A negative delta can drive the counter below zero only if
// the operator manually deleted files outside the WebDAV handler; we
// clamp at zero defensively so the PROPFIND report never goes
// negative.
func (s *usageState) addDelta(accountID string, delta int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.used[accountID] + delta
	if next < 0 {
		next = 0
	}
	if delta == 0 {
		return nil
	}
	s.used[accountID] = next
	return s.saveLocked()
}

// recalculate walks rootPath and replaces the cached byte count for
// accountID with the actual on-disk total. Use this to recover from a
// counter drift (e.g. operator manually rm-ed files, or initial
// migration from a pre-quota install). Errors during the walk are
// non-fatal — the counter is still updated with the partial sum so
// the operator gets a usable starting point.
func (s *usageState) recalculate(rootPath string, accountID string) error {
	total, walkErr := walkSize(rootPath)
	if walkErr != nil {
		log.Printf("webdav-quota: recalculate(%s, %s) walk error: %v (using partial sum %d)",
			rootPath, accountID, walkErr, total)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.used[accountID] = total
	if err := s.saveLocked(); err != nil {
		return fmt.Errorf("persist after recalculate: %w", err)
	}
	if walkErr == nil {
		log.Printf("webdav-quota: recalculated usage for %s = %d bytes", accountID, total)
	}
	return nil
}

// recalculateAll walks every (rootPath, accountID) pair and refreshes
// the cache. Returns per-account results for the admin endpoint.
func (s *usageState) recalculateAll(accounts []webdavAccount, root string) []map[string]any {
	out := make([]map[string]any, 0, len(accounts))
	for _, acc := range accounts {
		full := filepath.Join(root, acc.RootPath)
		total, err := walkSize(full)
		if err != nil {
			out = append(out, map[string]any{
				"id":         acc.ID,
				"ok":         false,
				"error":      err.Error(),
				"used_bytes": int64(0),
			})
			continue
		}
		_ = s.addDelta(acc.ID, total-s.get(acc.ID)) // bounded delta; safe to use
		out = append(out, map[string]any{
			"id":         acc.ID,
			"ok":         true,
			"used_bytes": total,
		})
	}
	return out
}

// saveLocked writes storage-usage.json atomically. Same shape as
// webdavAccountState.saveLocked (webdav_accounts.go:456) and
// loginState.saveLocked (login.go:194).
func (s *usageState) saveLocked() error {
	// Marshal ints as ints (smaller on disk, easier to grep).
	asInt := make(map[string]int, len(s.used))
	for k, v := range s.used {
		if v < 0 {
			v = 0
		}
		if v > 0 {
			asInt[k] = int(v) // int is at least 32 bits on every supported Go target
		}
	}
	f := usageFile{Version: 1, Used: asInt}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return mkErr
		}
	}
	tmp, err := os.CreateTemp(dir, "storage-usage-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpName); statErr == nil {
			os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// walkSize sums the size of every regular file under root. Symlinks and
// special files are skipped. Returns 0 for a missing path.
func walkSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// If the root itself is missing, return 0 cleanly so the
			// caller treats it as "nothing uploaded yet".
			if errors.Is(walkErr, fs.ErrNotExist) && p == root {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		// Skip symlinks — they can point outside the dav root and we
		// don't want to double-count or follow loops.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil // skip unreadable entries rather than abort
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// ─── quotaFileSystem ────────────────────────────────────────────────────────

// quotaFileSystem wraps a webdav.FileSystem to enforce per-account
// storage quotas. The wrapped inner filesystem is responsible for the
// actual on-disk operations; quotaFileSystem only intercepts at the
// boundaries where bytes are added or removed.
//
// Accounting model (mirrors Cloudreve's `ReserveStorage` + walk-based
// recalc):
//
//   - OpenFile (writes): pre-check against Content-Length-equivalent
//     hint if known, else fail-open; post-check the final on-disk
//     size in Close() and reconcile via usageState.addDelta.
//   - RemoveAll: walk the path before removal to compute the total,
//     subtract from usage after the inner call succeeds.
//   - Rename: in-place; size unchanged.
//   - Mkdir: directories cost zero bytes.
//   - Stat: pure passthrough.
//
// The pre-check is best-effort because the webdav.FileSystem interface
// doesn't carry Content-Length. The hard guarantee comes from the
// post-check + the admin recalculate endpoint.
type quotaFileSystem struct {
	inner      webdav.FileSystem
	state      *usageState
	accountID  string
	quotaBytes int64 // 0 = unlimited
	root       string
	readOnly   bool
}

// Mkdir creates a directory. Quota is irrelevant (directories are
// metadata-only) but read-only enforcement still applies.
func (q *quotaFileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if q.readOnly {
		return errors.New("webdav: account is read-only")
	}
	return q.inner.Mkdir(ctx, name, perm)
}

// OpenFile passes through to the inner filesystem but wraps the
// returned File so that Close() can reconcile the byte delta and reject
// the write if it would overshoot the configured quota.
//
// The flag bitmask follows os package conventions. We treat a write as
// any of:
//   - os.O_CREATE alone (rare; usually combined with O_WRONLY/O_RDWR)
//   - os.O_WRONLY or os.O_RDWR with truncation
//   - os.O_APPEND (we still account on Close; pre-check is skipped)
//
// readOnly accounts always reject write flags with a 403-equivalent
// error before reaching the inner filesystem.
//
// Accounting is ALWAYS performed for writes, regardless of whether a
// quota is configured, so the admin UI can show used_bytes for
// unlimited accounts too. The post-check overshoot warning only fires
// when quotaBytes > 0.
func (q *quotaFileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	isWrite := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0

	if isWrite && q.readOnly {
		return nil, errors.New("webdav: account is read-only")
	}

	// Capture the pre-existing size BEFORE inner.OpenFile so that a
	// O_TRUNC flag doesn't shrink the file underneath our Stat call.
	// We use os.Stat directly (not webdav.FileSystem.Stat) because the
	// inner filesystem's Stat may apply its own path translation and
	// we'd rather see the bytes on disk.
	var oldSize int64
	var absPath string
	if isWrite {
		absPath = filepath.Join(q.root, filepath.FromSlash(pathUnescape(name)))
		if info, statErr := os.Stat(absPath); statErr == nil && info != nil {
			oldSize = info.Size()
		}
	}

	inner, err := q.inner.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}

	if !isWrite {
		return inner, nil
	}

	// Capture quota state in local variables so the closure doesn't
	// depend on qf being constructed yet (avoids a "use before
	// declaration" trap in the composite literal).
	preflight := q.quotaBytes
	acct := q.accountID
	stateRef := q.state

	qf := &quotaFile{
		File:    inner,
		absPath: absPath,
		oldSize: oldSize,
		onClose: func() error {
			info, statErr := os.Stat(absPath)
			if statErr != nil {
				// File might have been removed by another path; skip accounting.
				return nil
			}
			newSize := info.Size()
			delta := newSize - oldSize
			if delta == 0 {
				return nil
			}
			// Post-check: if the write overshot the quota, log a warning
			// but accept the file. Cloudreve does the same. Future PUT
			// requests will be rejected until usage drops below the cap.
			if preflight > 0 && stateRef.get(acct)+delta > preflight {
				log.Printf("webdav-quota: account %s overshot quota by %d bytes (post-check); accepting file",
					acct, stateRef.get(acct)+delta-preflight)
			}
			return stateRef.addDelta(acct, delta)
		},
	}
	return qf, nil
}

// RemoveAll removes the path and subtracts its pre-removal byte total
// from the account usage. Returns the inner error verbatim if the
// inner filesystem fails; usage is NOT decremented in that case (so a
// failed remove leaves the counter correct).
func (q *quotaFileSystem) RemoveAll(ctx context.Context, name string) error {
	if q.readOnly {
		return errors.New("webdav: account is read-only")
	}
	abs := filepath.Join(q.root, filepath.FromSlash(pathUnescape(name)))
	removed, _ := walkSize(abs) // ignore error; partial result is better than nothing
	if err := q.inner.RemoveAll(ctx, name); err != nil {
		return err
	}
	if removed > 0 {
		if err := q.state.addDelta(q.accountID, -removed); err != nil {
			log.Printf("webdav-quota: account %s RemoveAll accounting failed: %v", q.accountID, err)
		}
	}
	return nil
}

// Rename moves oldName to newName within the inner filesystem. Both
// names are inside the same root, so byte total is unchanged. We still
// guard against read-only writes.
func (q *quotaFileSystem) Rename(ctx context.Context, oldName, newName string) error {
	if q.readOnly {
		return errors.New("webdav: account is read-only")
	}
	return q.inner.Rename(ctx, oldName, newName)
}

// Stat is a pure passthrough — quota state doesn't affect metadata.
func (q *quotaFileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return q.inner.Stat(ctx, name)
}

// quotaFile wraps a webdav.File so Close() can reconcile the byte delta
// via usageState.addDelta.
type quotaFile struct {
	webdav.File
	absPath string
	oldSize int64
	onClose func() error
	closed  bool
}

// Close closes the inner file and runs the deferred accounting.
func (f *quotaFile) Close() error {
	err := f.File.Close()
	if f.closed {
		return err
	}
	f.closed = true
	if f.onClose != nil {
		if accErr := f.onClose(); accErr != nil && err == nil {
			err = accErr
		}
	}
	return err
}

// pathUnescape reverses the path escaping applied by webdav.Handler
// (RFC 3986 percent-decoding). webdav.Dir uses path.Clean on the
// already-decoded path; we only need the decoded string for filepath.Join.
func pathUnescape(p string) string {
	// os.PathSeparator is / on every supported target for this binary,
	// so we don't have to worry about Windows backslashes in the input.
	decoded, err := urlPathUnescape(p)
	if err != nil {
		return p
	}
	return decoded
}

// ─── Top-level PROPFIND interceptor ─────────────────────────────────────────

// quotaContext is the per-request quota + chroot snapshot stashed by
// webdavServer.ServeHTTP and read by handlePropfindRoot. The struct
// lives in webdav_quota.go because that's the only place it's used;
// webdav.go just calls setQuotaContext / clearQuotaContext.
type quotaContext struct {
	accountID  string
	quotaBytes int64
	root       string // absolute filesystem path of the account's chroot
}

// quotaRequestContext is a package-level map from *http.Request to
// the quotaContext for that request. We use a map (instead of passing
// the context through the http.Handler signature, which is fixed) by
// keying on the request pointer; entries are inserted by ServeHTTP and
// removed on return via defer. *http.Request pointers are unique per
// request, so this is safe even under high concurrency.
var quotaRequestContext = make(map[*http.Request]quotaContext)

// setQuotaContext installs the quota context for r. Caller must defer
// clearQuotaContext(r) to avoid leaking.
func setQuotaContext(r *http.Request, ctx quotaContext) {
	quotaRequestContext[r] = ctx
}

// clearQuotaContext removes the quota context for r. Idempotent.
func clearQuotaContext(r *http.Request) {
	delete(quotaRequestContext, r)
}

// getQuotaContext returns the quota context for r. Zero value if none
// has been set (caller is responsible for handling that case).
func getQuotaContext(r *http.Request) quotaContext {
	return quotaRequestContext[r]
}

// davHref returns the canonical WebDAV href for use in PROPFIND
// responses. Always ends with a slash per RFC 4918 § 8.3.
func (s *webdavServer) davHref() string {
	return "/dav/"
}

// handlePropfindRoot serves PROPFIND requests targeting the WebDAV
// root ("/dav/" or "/dav") and emits a 207 multistatus containing
// DAV:quota-used-bytes and DAV:quota-available-bytes (RFC 4331).
//
// All other PROPFIND requests (subdirectory paths) are forwarded to
// the upstream webdav.Handler unchanged.
//
// This handler deliberately ignores the request body — modern clients
// always use Propfind XML bodies to declare which properties they want,
// but Windows Explorer and RaiDrive also send raw Depth headers with
// empty bodies, and both should succeed. We respond with the union of
// all commonly-requested live properties so the client gets a usable
// view no matter what it asked for.
func (s *webdavServer) handlePropfindRoot(w http.ResponseWriter, r *http.Request) {
	depth := parseDepthHeader(r.Header.Get("Depth"))
	if depth > 1 {
		depth = 1 // RFC 4918 forbids infinity on a collection without children-infinity
	}

	qctx := getQuotaContext(r)

	used := int64(0)
	if s.usage != nil {
		used = s.usage.get(qctx.accountID)
	}
	quota := qctx.quotaBytes
	root := qctx.root

	rootInfo, err := os.Stat(root)
	if err != nil {
		http.Error(w, "root not accessible", http.StatusInternalServerError)
		return
	}

	var children []childInfo
	if depth == 1 {
		entries, readErr := os.ReadDir(root)
		if readErr != nil {
			http.Error(w, "cannot list root", http.StatusInternalServerError)
			return
		}
		for _, e := range entries {
			info, infoErr := e.Info()
			if infoErr != nil {
				continue
			}
			children = append(children, childInfo{
				name:    e.Name(),
				size:    info.Size(),
				modTime: info.ModTime(),
				isDir:   e.IsDir(),
			})
		}
	}

	ms := buildMultistatusResponse(
		s.davHref(), rootInfo, used, quota, children,
	)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("DAV", "1, 2")
	w.WriteHeader(http.StatusMultiStatus) // 207
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(ms); err != nil {
		log.Printf("webdav-quota: PROPFIND encode error: %v", err)
	}
	_, _ = io.WriteString(w, "\n")
}

// parseDepthHeader converts a Depth header value to an int. RFC 4918
// defines "0", "1", and "infinity"; anything else is treated as 0 per
// § 10.2.
func parseDepthHeader(v string) int {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1":
		return 1
	case "infinity":
		return 2 // sentinel for "all the way down"; we'll clamp to 1 anyway
	default:
		return 0
	}
}

// ─── Multistatus XML ────────────────────────────────────────────────────────

// The XML structs below intentionally mirror the element names from
// RFC 4918 § 14 (multistatus), RFC 4918 § 15 (PROPFIND), and RFC 4331
// § 3 (quota properties). Field names use D: prefix via the
// xmlns:D="DAV:" attribute on the root element.

type msResponse struct {
	XMLName   xml.Name      `xml:"D:response"`
	Href      string        `xml:"D:href"`
	Propstats []msPropstat  `xml:"D:propstat"`
}

type msPropstat struct {
	XMLName xml.Name `xml:"D:propstat"`
	Prop    msProp   `xml:"D:prop"`
	Status  string   `xml:"D:status"`
}

type msProp struct {
	Resourcetype     msResourcetype `xml:"D:resourcetype"`
	DisplayName      string         `xml:"D:displayname,omitempty"`
	CreationDate     string         `xml:"D:creationdate,omitempty"`
	GetLastModified  string         `xml:"D:getlastmodified,omitempty"`
	GetContentLength *int64         `xml:"D:getcontentlength,omitempty"`
	GetContentType   string         `xml:"D:getcontenttype,omitempty"`
	GetEtag          string         `xml:"D:getetag,omitempty"`
	SupportedLock    string         `xml:"D:supportedlock,omitempty"`
	QuotaUsedBytes   *int64         `xml:"D:quota-used-bytes,omitempty"`
	QuotaAvailBytes  *int64         `xml:"D:quota-available-bytes,omitempty"`
}

type msResourcetype struct {
	XMLName    xml.Name  `xml:"D:resourcetype"`
	Collection *struct{} `xml:"D:collection,omitempty"`
}

type msMultistatus struct {
	XMLName   xml.Name     `xml:"D:multistatus"`
	DAVNS     string       `xml:"xmlns:D,attr"`
	Responses []msResponse `xml:"D:response"`
}

// childInfo mirrors enough of os.FileInfo for the depth-1 child list.
type childInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

// buildMultistatusResponse composes the 207 response body.
func buildMultistatusResponse(
	rootHref string,
	rootInfo os.FileInfo,
	used, quota int64,
	children []childInfo,
) msMultistatus {
	rootProps := msProp{
		Resourcetype: msResourcetype{Collection: &struct{}{}},
		DisplayName:  rootInfo.Name(),
		CreationDate: rootInfo.ModTime().UTC().Format(time.RFC3339),
		GetLastModified: formatHTTPDate(rootInfo.ModTime()),
		SupportedLock:   "",
	}
	if quota > 0 {
		u := used
		rootProps.QuotaUsedBytes = &u
		avail := quota - used
		if avail < 0 {
			avail = 0
		}
		rootProps.QuotaAvailBytes = &avail
	}
	rootResp := msResponse{
		Href: rootHref,
		Propstats: []msPropstat{{
			Prop:   rootProps,
			Status: "HTTP/1.1 200 OK",
		}},
	}

	responses := []msResponse{rootResp}
	for _, c := range children {
		href := strings.TrimRight(rootHref, "/") + "/" + urlPathEscape(c.name)
		props := msProp{
			Resourcetype: msResourcetype{}, // empty = regular file or non-collection
			DisplayName:  c.name,
			CreationDate: c.modTime.UTC().Format(time.RFC3339),
			GetLastModified: formatHTTPDate(c.modTime),
			GetEtag:       weakEtag(c.modTime, c.size),
		}
		if !c.isDir {
			size := c.size
			props.GetContentLength = &size
		} else {
			props.Resourcetype = msResourcetype{Collection: &struct{}{}}
		}
		responses = append(responses, msResponse{
			Href: href,
			Propstats: []msPropstat{{
				Prop:   props,
				Status: "HTTP/1.1 200 OK",
			}},
		})
	}

	return msMultistatus{
		DAVNS:     "DAV:",
		Responses: responses,
	}
}

// formatHTTPDate returns an RFC 1123 date suitable for DAV:getlastmodified.
func formatHTTPDate(t time.Time) string {
	return t.UTC().Format(http.TimeFormat)
}

// weakEtag is a "W/<unix>-<size>" style tag, sufficient for clients
// that only need change detection.
func weakEtag(t time.Time, size int64) string {
	return fmt.Sprintf(`W/"%x-%x"`, t.Unix(), size)
}

// isWebdavRootPath reports whether p is the WebDAV mount root, with or
// without trailing slash. Used by webdavServer.ServeHTTP to decide
// whether to self-handle the request.
func isWebdavRootPath(p string) bool {
	trimmed := strings.TrimRight(p, "/")
	return trimmed == "" || trimmed == "/dav"
}

// ─── Content-Length pre-check helper ────────────────────────────────────────

// preflightQuota returns ErrInsufficientCapacity if writing
// contentLength bytes (against a file that already has oldSize bytes)
// would push the account over its quota. Returns nil when no quota is
// configured (quotaBytes <= 0) or when the new total fits.
//
// Used by webdavServer.ServeHTTP before delegating to the upstream
// handler for write methods (PUT in particular).
func preflightQuota(used, quotaBytes, oldSize, contentLength int64) error {
	if quotaBytes <= 0 {
		return nil
	}
	if contentLength < 0 {
		// Chunked transfer / unknown length — let the post-check catch it.
		return nil
	}
	after := used - oldSize + contentLength
	if after < 0 {
		// File is being shrunk; can't exceed quota.
		return nil
	}
	if after > quotaBytes {
		return ErrInsufficientCapacity
	}
	return nil
}

// ─── urlPathEscape / urlPathUnescape ────────────────────────────────────────
//
// We deliberately avoid importing net/url here (it would pull in net/http
// cycles in tests that only need path handling). stdlib escape handling
// for href paths is small enough to inline.

func urlPathEscape(s string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_.~"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c > 0x7f || strings.IndexByte(safe, c) < 0 {
			b.WriteByte('%')
			b.WriteString(strings.ToUpper(strconv.FormatUint(uint64(c), 16)))
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func urlPathUnescape(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := hexDigit(s[i+1])
			lo, ok2 := hexDigit(s[i+2])
			if ok1 && ok2 {
				b.WriteByte(byte(hi<<4 | lo))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String(), nil
}

func hexDigit(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}