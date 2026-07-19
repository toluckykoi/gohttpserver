package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/webdav"
)

// webdavServer wraps the standard golang.org/x/net/webdav handler with
// HTTP Basic Auth, a master enable switch, per-account chroot, and
// per-account quota enforcement.
//
// The upstream webdav.Handler is responsible for OPTIONS / PROPFIND /
// PUT / DELETE / MKCOL / MOVE / COPY / PROPPATCH / LOCK / UNLOCK per
// RFC 4918. We extend it in three places:
//
//   1. handlePropfindRoot (webdav_quota.go) — top-level PROPFIND is
//      self-handled so we can inject DAV:quota-used-bytes and
//      DAV:quota-available-bytes (RFC 4331). All other PROPFIND
//      requests pass through to upstream.
//   2. quotaFileSystem (webdav_quota.go) — replaces the bare
//      webdav.Dir so writes are intercepted for quota enforcement
//      and byte-level accounting.
//   3. preflightQuota check in ServeHTTP — fast rejection of
//      over-quota PUTs based on Content-Length before delegating to
//      upstream.
type webdavServer struct {
	root     string
	accounts *webdavAccountState
	usage    *usageState
}

// newWebdavServer constructs the server. The returned handler is safe
// to register on a router immediately; it reads the enabled flag and
// account list from `accounts` lazily on every request.
//
// `root` is the --root filesystem root; the per-account chroot is
// applied at the file-system layer by joining onto RootPath.
func newWebdavServer(root string, accounts *webdavAccountState, usage *usageState) *webdavServer {
	return &webdavServer{
		root:     root,
		accounts: accounts,
		usage:    usage,
	}
}

// ServeHTTP gates the upstream webdav.Handler behind HTTP Basic Auth
// and per-account quota enforcement.
//
// The first account whose username + password matches the
// Authorization header is used; if no match is found we return 401
// with a Basic challenge so the client prompts for credentials. The
// upstream webdav.Handler takes care of OPTIONS / PROPFIND / PUT /
// DELETE / MKCOL / MOVE / COPY / PROPPATCH / LOCK / UNLOCK per
// RFC 4918, except:
//
//   - Top-level PROPFIND is intercepted to inject quota properties.
//   - PUTs that exceed the per-account quota return 507.
//   - Per-account chroot is applied via quotaFileSystem.
func (s *webdavServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.accounts.isEnabled() {
		http.Error(w, "WebDAV is disabled", http.StatusServiceUnavailable)
		return
	}

	// Normalise the trailing-slash-less collection path ("/dav" → "/dav/").
	// GNOME/gvfs probes "/dav" during discovery; without this the upstream
	// handler (Prefix "/dav/") can't strip the prefix and returns 404.
	if r.URL.Path == "/dav" {
		r.URL.Path = "/dav/"
	}

	user, pass, ok := r.BasicAuth()
	if !ok || user == "" || pass == "" {
		writeBasicChallenge(w)
		http.Error(w, "WebDAV authentication required", http.StatusUnauthorized)
		return
	}
	acc, ok := s.accounts.verifyWebdav(user, pass)
	if !ok {
		log.Printf("webdav: auth failed for user=%q from %s (%s %s)",
			user, getRealIP(r), r.Method, r.URL.Path)
		writeBasicChallenge(w)
		http.Error(w, "WebDAV authentication required", http.StatusUnauthorized)
		return
	}

	// Log only mutating operations (uploads / deletes / moves etc.) so
	// operators see meaningful WebDAV activity. Read-only browsing
	// methods (PROPFIND / OPTIONS / GET / HEAD / LOCK / UNLOCK) fire
	// constantly during navigation and would flood the log, so they're
	// skipped.
	switch r.Method {
	case "PUT", "DELETE", "MKCOL", "MOVE", "COPY", "PROPPATCH":
		log.Printf("webdav: %s %s by user=%q (remark=%q) from %s",
			r.Method, r.URL.Path, acc.Username, acc.Remark, getRealIP(r))
	}

	// Per-account chroot: mount the inner webdav.Dir at
	//   filepath.Join(s.root, acc.RootPath)
	// so all subsequent file operations are confined to the account's
	// root_path. The upstream handler still strips the configured
	// /dav/ Prefix before reaching the FileSystem, so the path
	// rewriting is correct.
	accountRoot := filepath.Join(s.root, acc.RootPath)
	if !strings.HasSuffix(accountRoot, string(filepath.Separator)) {
		accountRoot += string(filepath.Separator)
	}

	// Stash the matched account's quota state so handlePropfindRoot
	// (and any other downstream reader) can look it up via the
	// request pointer. Cleared by defer below.
	setQuotaContext(r, quotaContext{
		accountID:  acc.ID,
		quotaBytes: acc.QuotaBytes,
		root:       accountRoot,
	})
	defer clearQuotaContext(r)

	// Top-level PROPFIND is self-handled for quota injection.
	if r.Method == "PROPFIND" && isWebdavRootPath(r.URL.Path) {
		s.handlePropfindRoot(w, r)
		return
	}

	// Pre-flight quota check for PUT: use Content-Length if the client
	// declared one. The post-check inside quotaFileSystem.OpenFile
	// catches clients that lied or used chunked transfer.
	if r.Method == "PUT" && acc.QuotaBytes > 0 {
		if err := s.preflightPut(r, acc, accountRoot); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, ErrInsufficientCapacity) {
				status = http.StatusInsufficientStorage // 507
			}
			http.Error(w, err.Error(), status)
			return
		}
	}

	// Build a request-scoped handler with the account's chroot and
	// quota wrapper. The handler is constructed per request because
	// the FileSystem closure needs the matched account's id + quota.
	inner := webdav.Dir(accountRoot)
	qfs := &quotaFileSystem{
		inner:      inner,
		state:      s.usage,
		accountID:  acc.ID,
		quotaBytes: acc.QuotaBytes,
		root:       accountRoot,
		readOnly:   acc.ReadOnly,
	}
	handler := &webdav.Handler{
		Prefix:     "/dav/",
		FileSystem: qfs,
		LockSystem: webdav.NewMemLS(),
	}
	handler.ServeHTTP(w, r)
}

// preflightPut runs a fast quota check using the request's
// Content-Length. The FileSystem wrapper handles the post-write
// accounting; this is just to fail-fast on declared sizes.
func (s *webdavServer) preflightPut(r *http.Request, acc webdavAccount, accountRoot string) error {
	if s.usage == nil || r.ContentLength < 0 {
		return nil
	}
	used := s.usage.get(acc.ID)
	// Look up the old file size so overwriting a 2GB file with 1KB
	// doesn't get rejected as +2GB.
	var oldSize int64
	target := filepath.Join(accountRoot, urlPathClean(r.URL.Path))
	if info, err := os.Stat(target); err == nil && info != nil {
		oldSize = info.Size()
	}
	return preflightQuota(used, acc.QuotaBytes, oldSize, r.ContentLength)
}

// writeBasicChallenge sets the headers needed to make Windows / Finder /
// RaiDrive prompt for credentials rather than fail silently.
func writeBasicChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="gohttpserver webdav"`)
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
}

// urlPathClean strips the /dav/ prefix from p and runs filepath.Clean
// on the remainder, producing a safe relative path for the inner
// webdav.Dir. Empty result means "the chroot root itself".
func urlPathClean(p string) string {
	const prefix = "/dav/"
	switch {
	case strings.HasPrefix(p, prefix):
		p = strings.TrimPrefix(p, prefix)
	case p == "/dav":
		p = ""
	}
	if p == "" {
		return "."
	}
	return filepath.ToSlash(filepath.Clean("/" + p))
}