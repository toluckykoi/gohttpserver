package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"regexp"

	"github.com/go-yaml/yaml"
	"github.com/gorilla/mux"
	"github.com/shogo82148/androidbinary/apk"
)

const YAMLCONF = ".ghs.yml"

const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://www.google-analytics.com https://www.googletagmanager.com; style-src 'self' 'unsafe-inline'; img-src 'self' data: https://www.google-analytics.com https://www.googletagmanager.com; font-src 'self' data:; media-src 'self'; connect-src 'self' https://www.google-analytics.com https://www.googletagmanager.com; form-action 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'"

type ApkInfo struct {
	PackageName  string `json:"packageName"`
	MainActivity string `json:"mainActivity"`
	Version      struct {
		Code int    `json:"code"`
		Name string `json:"name"`
	} `json:"version"`
}

type IndexFileItem struct {
	Path string
	Info os.FileInfo
}

type Directory struct {
	size  map[string]int64
	mutex *sync.RWMutex
}

type HTTPStaticServer struct {
	Root             string
	Prefix           string
	Upload           bool
	Delete           bool
	Title            string
	Theme            string
	PlistProxy       string
	GoogleTrackerID  string
	AuthType         string
	DeepPathMaxDepth int
	NoIndex          bool

	indexes []IndexFileItem
	m       *mux.Router
	bufPool sync.Pool // use sync.Pool caching buf to reduce gc ratio
}

func NewHTTPStaticServer(root string, noIndex bool) *HTTPStaticServer {
	// if root == "" {
	// 	root = "./"
	// }
	// root = filepath.ToSlash(root)
	root = filepath.ToSlash(filepath.Clean(root))
	if !strings.HasSuffix(root, "/") {
		root = root + "/"
	}
	log.Printf("root path: %s\n", root)
	m := mux.NewRouter()
	s := &HTTPStaticServer{
		Root:  root,
		Theme: "black",
		m:     m,
		bufPool: sync.Pool{
			New: func() any { return make([]byte, 32*1024) },
		},
		NoIndex: noIndex,
	}

	if !noIndex {
		go func() {
			time.Sleep(1 * time.Second)
			for {
				startTime := time.Now()
				log.Println("Started making search index")
				s.makeIndex()
				log.Printf("Completed search index in %v", time.Since(startTime))
				//time.Sleep(time.Second * 1)
				time.Sleep(time.Minute * 10)
			}
		}()
	}

	// routers for Apple *.ipa
	m.HandleFunc("/-/ipa/plist/{path:.*}", s.hPlist)
	m.HandleFunc("/-/ipa/link/{path:.*}", s.hIpaLink)
	m.HandleFunc("/-/video-player/{path:.*}", s.hVideoPlayer)

	// Multi-select archive. Frontend posts a JSON body listing each
	// selected path; we stream back a single zip preserving each entry's
	// basename as the top-level name in the archive.
	m.HandleFunc("/-/zip", s.hZipMulti).Methods("POST")

	// Offline URL download. Frontend posts form fields `url` (the
	// remote resource to fetch) and `to` (the basename to save as
	// under the current directory). SSRF protection lives inside the
	// handler — we block private/loopback IPs before opening the
	// remote connection.
	m.HandleFunc("/-/fetch", s.hFetch).Methods("POST")

	// File info API: returns metadata, hashes, and for .apk/.ipa files
	// also extracts package-level information.
	m.HandleFunc("/-/info/{path:.*}", s.hInfo).Methods("GET")
	// Android-package-specific info endpoint (mirrors /-/info/ for .apk).
	m.HandleFunc("/-/apk/info/{path:.*}", s.hInfo).Methods("GET")

	m.HandleFunc("/{path:.*}", s.hIndex).Methods("GET", "HEAD")
	m.HandleFunc("/{path:.*}", s.hUploadOrMkdir).Methods("POST")
	m.HandleFunc("/{path:.*}", s.hEdit).Methods("PUT")
	m.HandleFunc("/{path:.*}", s.hDelete).Methods("DELETE")
	return s
}

func (s *HTTPStaticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Defense-in-depth for uploaded content and README previews.
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	s.m.ServeHTTP(w, r)
}

// Return real path with Seperator(/)
func (s *HTTPStaticServer) getRealPath(r *http.Request) string {
	return s.resolvePath(mux.Vars(r)["path"])
}

// resolvePath turns a URL path (already URL-decoded by gorilla/mux) into an
// absolute, slash-normalised filesystem path under s.Root. It is shared by
// getRealPath (which feeds it from a route var) and by handlers like
// hZipMulti that take paths from a JSON body. filepath.Clean collapses any
// ".." segments so a caller cannot escape s.Root.
func (s *HTTPStaticServer) resolvePath(urlPath string) string {
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	cleanPath := filepath.Clean(urlPath) // prevent .. for safe issues
	relativePath, err := filepath.Rel(s.Prefix, cleanPath)
	if err != nil {
		relativePath = cleanPath
	}
	return filepath.ToSlash(filepath.Join(s.Root, relativePath))
}

func (s *HTTPStaticServer) hIndex(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	realPath := s.getRealPath(r)
	if r.FormValue("json") == "true" {
		s.hJSONList(w, r)
		return
	}

	if r.FormValue("op") == "info" {
		s.hInfo(w, r)
		return
	}

	if r.FormValue("op") == "archive" {
		s.hZip(w, r)
		return
	}

	log.Println("GET", path, realPath)
	if r.FormValue("raw") == "false" || isDir(realPath) {
		if r.Method == "HEAD" {
			return
		}
		// Serve the Vue 3 frontend for directory listings
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		f, err := FrontendAssets.Open("index.html")
		if err != nil {
			http.Error(w, "Frontend not built", http.StatusNotFound)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		w.Write(data)
	} else {
		if filepath.Base(path) == YAMLCONF {
			auth := s.readAccessConf(realPath)
			if !auth.Delete {
				http.Error(w, "Security warning, not allowed to read", http.StatusForbidden)
				return
			}
		}
		if r.FormValue("download") == "true" {
			w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filepath.Base(path)))
		}
		http.ServeFile(w, r, realPath)
	}
}

func (s *HTTPStaticServer) hDelete(w http.ResponseWriter, req *http.Request) {
	path := mux.Vars(req)["path"]
	realPath := s.getRealPath(req)
	// path = filepath.Clean(path) // for safe reason, prevent path contain ..
	auth := s.readAccessConf(realPath)
	if !auth.canDelete(req) {
		http.Error(w, "Delete forbidden", http.StatusForbidden)
		return
	}

	// TODO: path safe check
	err := os.RemoveAll(realPath)
	if err != nil {
		pathErr, ok := err.(*os.PathError)
		if ok {
			http.Error(w, pathErr.Op+" "+path+": "+pathErr.Err.Error(), 500)
		} else {
			http.Error(w, err.Error(), 500)
		}
		return
	}
	// Drop cached sizes — files just disappeared from disk.
	invalidateDirSizeCache()
	w.Write([]byte("Success"))
}

func (s *HTTPStaticServer) hUploadOrMkdir(w http.ResponseWriter, req *http.Request) {
	dirpath := s.getRealPath(req)

	// check auth
	auth := s.readAccessConf(dirpath)
	if !auth.canUpload(req) {
		http.Error(w, "Upload forbidden", http.StatusForbidden)
		return
	}

	file, header, err := req.FormFile("file")

	if _, err := os.Stat(dirpath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirpath, os.ModePerm); err != nil {
			log.Println("Create directory:", err)
			http.Error(w, "Directory create "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if file == nil { // only mkdir
		w.Header().Set("Content-Type", "application/json;charset=utf-8")
		json.NewEncoder(w).Encode(map[string]any{
			"success":     true,
			"destination": dirpath,
		})
		return
	}

	if err != nil {
		log.Println("Parse form file:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		file.Close()
		req.MultipartForm.RemoveAll() // Seen from go source code, req.MultipartForm not nil after call FormFile(..)
	}()

	filename := req.FormValue("filename")
	if filename == "" {
		filename = header.Filename
	}

	// `path` is the relative-path field used by folder uploads. When
	// set, it overrides the flat `filename`/`header.Filename` semantics
	// and lets the caller preserve directory structure. We normalise
	// separators to "/" first so the segment check below works on both
	// POSIX and Windows.
	relPath := req.FormValue("path")
	var dstPath string
	if relPath != "" {
		cleaned := path.Clean(strings.ReplaceAll(relPath, "\\", "/"))
		// Reject absolute paths on either OS: path.IsAbs covers "/..."
		// (POSIX and Windows-style), filepath.IsAbs additionally catches
		// Windows drive letters like "C:/foo". Both together close the
		// hole regardless of where the server runs.
		if path.IsAbs(cleaned) || filepath.IsAbs(cleaned) {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
		for _, seg := range strings.Split(cleaned, "/") {
			if err := checkPathSegment(seg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		dstPath = filepath.Join(dirpath, filepath.FromSlash(cleaned))
		// Create any intermediate directories the relative path implies.
		// The existing os.MkdirAll(dirpath) above only guarantees the
		// URL-route directory exists; for folder uploads we may need to
		// create "MyFolder/" and "MyFolder/sub/" too.
		if parent := filepath.Dir(dstPath); parent != dirpath {
			if err := os.MkdirAll(parent, os.ModePerm); err != nil {
				log.Println("Create parent directory:", err)
				http.Error(w, "Directory create "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		if err := checkFilename(filename); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		dstPath = filepath.Join(dirpath, filename)
	}

	// Large file (>32MB) will store in tmp directory
	// The quickest operation is call os.Move instead of os.Copy
	// Note: it seems not working well, os.Rename might be failed

	var copyErr error
	// if osFile, ok := file.(*os.File); ok && fileExists(osFile.Name()) {
	// 	tmpUploadPath := osFile.Name()
	// 	osFile.Close() // Windows can not rename opened file
	// 	log.Printf("Move %s -> %s", tmpUploadPath, dstPath)
	// 	copyErr = os.Rename(tmpUploadPath, dstPath)
	// } else {
	dst, err := os.Create(dstPath)
	if err != nil {
		log.Println("Create file:", err)
		http.Error(w, "File create "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Note: very large size file might cause poor performance
	// _, copyErr = io.Copy(dst, file)
	buf := s.bufPool.Get().([]byte)
	defer s.bufPool.Put(buf)
	_, copyErr = io.CopyBuffer(dst, file, buf)
	dst.Close()
	// }
	if copyErr != nil {
		log.Println("Handle upload file:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json;charset=utf-8")

	// Drop cached directory sizes — a file just landed on disk and the
	// next directory listing should reflect it without waiting for the
	// 10-minute index rebuild.
	invalidateDirSizeCache()

	if req.FormValue("unzip") == "true" {
		// Streaming NDJSON progress: switch the response to chunked
		// transfer and emit one JSON line per file as it is extracted.
		// The terminal line carries the final success/description so the
		// client can resolve its upload promise.
		w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
		// Disable nginx response buffering so progress events reach the
		// client in real time instead of being held until the handler
		// returns. No-op for direct connections.
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, _ := w.(http.Flusher)

		writeLine := func(payload string) {
			io.WriteString(w, payload)
			io.WriteString(w, "\n")
			if flusher != nil {
				flusher.Flush()
			}
		}

		err = unzipFile(req.Context(), dstPath, dirpath, func(idx, total int, name string) {
			// Best-effort JSON-encode the file name; invalid UTF-8
			// sequences are replaced rather than aborting extraction.
			encoded, _ := json.Marshal(name)
			writeLine(fmt.Sprintf(`{"phase":"unzip","current":%d,"total":%d,"file":%s}`, idx, total, string(encoded)))
		})
		// Only remove the original archive after a successful extraction.
		// The previous behaviour of always-remove would silently destroy
		// non-zip uploads that the client sent with unzip=true (e.g. a
		// mixed batch where the user ticked "extract after upload" but
		// also included regular files). On failure the file stays on
		// disk so the user can retry or extract it manually.
		if err == nil {
			os.Remove(dstPath)
		}
		message := "success"
		if err != nil {
			message = err.Error()
		}
		writeLine(fmt.Sprintf(`{"phase":"done","success":%v,"description":%q}`, err == nil, message))
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"destination": dstPath,
	})
}

// maxEditSize caps how large a PUT body we accept for in-browser file
// edits. Browsers reasonably edit text files (markdown, JSON, code),
// not multi-GB blobs — anything bigger should be downloaded and
// re-uploaded as a whole file. 5 MiB matches Element Plus's textarea
// ergonomics: a comfortable ceiling for source files, well under
// memory pressure on the server.
const maxEditSize int64 = 5 * 1024 * 1024

// hEdit handles PUT requests against an existing file. The request
// body becomes the new file contents. Use case: the frontend's
// in-browser editor saves changes for small text files (code,
// markdown, config). For multi-MB files, the upload pipeline is the
// right path; PUT is intentionally size-capped.
//
// Auth: same as upload — the user must have upload permission on the
// containing directory. We do not require delete permission; PUT
// modifies, doesn't remove. The existing .ghs.yml `upload` flag is
// the natural gate.
func (s *HTTPStaticServer) hEdit(w http.ResponseWriter, req *http.Request) {
	dstPath := s.getRealPath(req)

	// Reject obvious path escapes early — the resolvePath call below
	// already cleans the route var, but a PUT against "../../etc/foo"
	// would resolve outside Root and we'd then be writing to a file
	// the user can't browse to. Belt and braces.
	if strings.Contains(req.URL.Path, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	// Reject writes to directories. PUT against a directory is a 400
	// because mkdir is POST + no multipart, and reusing PUT for both
	// would muddle the semantics of each handler.
	fi, err := os.Stat(dstPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if fi.IsDir() {
		http.Error(w, "Cannot edit a directory", http.StatusBadRequest)
		return
	}

	// Auth check: same gate as upload. We use the destination's parent
// dir for the .ghs.yml lookup so an admin granting upload on a
// directory implicitly grants edit on its files.
//
// canUpload uses r.FormValue("token") which calls r.ParseForm() and
// drains the body when Content-Type is application/x-www-form-urlencoded
// — that would leave nothing for the file write. PUT bodies here
// ARE the file content, so we extract the token from the URL query
// (or the X-Token header) and never touch the body.
	ac := s.readAccessConf(filepath.Dir(dstPath))
	token := req.URL.Query().Get("token")
	if token == "" {
		token = req.Header.Get("X-Token")
	}
	var allowed bool
	if token != "" {
		allowed = ac.canUploadByToken(token)
	} else {
		allowed = ac.canUploadSession(req)
	}
	if !allowed {
		http.Error(w, "Edit forbidden", http.StatusForbidden)
		return
	}

	// Size cap before we copy anything — reading the body unbounded
	// would let a client exhaust server memory with a giant PUT.
	if req.ContentLength > maxEditSize {
		http.Error(w, fmt.Sprintf("File too large to edit (max %d bytes); re-upload instead", maxEditSize), http.StatusRequestEntityTooLarge)
		return
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, "File create "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	buf := s.bufPool.Get().([]byte)
	defer s.bufPool.Put(buf)
	written, copyErr := io.CopyBuffer(dst, req.Body, buf)
	if copyErr != nil {
		// Roll back partial writes — the file is now shorter than it
		// was on disk. Truncate to its previous size rather than
		// leaving a half-written file in place.
		if cerr := dst.Close(); cerr == nil {
			os.Truncate(dstPath, fi.Size())
		}
		http.Error(w, copyErr.Error(), http.StatusInternalServerError)
		return
	}
	// Defence-in-depth: if Content-Length was missing or lied, refuse
	// to commit a write that exceeds the cap. Truncate and 413.
	if written > maxEditSize {
		os.Truncate(dstPath, fi.Size())
		http.Error(w, fmt.Sprintf("File too large to edit (max %d bytes)", maxEditSize), http.StatusRequestEntityTooLarge)
		return
	}

	// The file's modification time and parent dir's size-cache just
	// changed — drop the cache so the next listing reflects reality.
	invalidateDirSizeCache()

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"destination": dstPath,
		"size":        written,
	})
}

// maxFetchSize caps how large a remote resource we accept. Mirrors
// the edit cap: anything bigger should be downloaded by the user's
// own browser, not proxied through this server. 1 GiB is generous
// for typical use (disk images, big PDFs) without letting a single
// request hold a connection open for hours.
const maxFetchSize int64 = 1 << 30 // 1 GiB

// fetchTimeout is the upper bound on a remote HTTP request,
// including DNS + connect + read time. Larger than typical because
// some hosts throttle downloads; smaller than "indefinite" so a hung
// connection doesn't pin a worker.
const fetchTimeout = 5 * time.Minute

// safeDialContext refuses to dial loopback, link-local, multicast, or
// RFC1918 private addresses. Without this, a POST to /-/fetch with
// url=http://127.0.0.1:6379/... would let the server attack itself
// or other services on the host network. The check runs after DNS
// resolution, so a hostname that resolves to a private IP (DNS
// rebinding attempt) is also caught.
//
// "Why not just block the literal IP in the URL string?": DNS
// rebinding means the URL can be http://attacker.com/... where
// attacker.com's A record flips to 127.0.0.1 between the URL parse
// and the connect. Resolving and validating in the DialContext
// closes that window.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
			isPrivateIPv4(ip) {
			return nil, fmt.Errorf("refusing to connect to private/loopback address %s", ip)
		}
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(host, port))
}

func isPrivateIPv4(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 169 && ip4[1] == 254: // link-local
			return true
		case ip4[0] == 127: // loopback in IPv4-mapped form
			return true
		}
	}
	// Unique local addresses fc00::/7 (IPv6 private)
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	return false
}

// hFetch downloads a remote URL to a file under the current route.
// Form fields:
//   url  — the http(s) URL to fetch (required)
//   to   — destination basename under the current directory (required;
//          path separators and `..` rejected the same way as uploads)
//
// SSRF: the URL is parsed; only http/https are accepted; the dial
// address is validated against the safe-dial rules above so an
// attacker can't make the server talk to its own loopback.
//
// Auth: same gate as upload on the current directory.
//
// Stream: response body is copied straight to disk with a 32 KiB
// scratch buffer (same pattern as uploads) so a 1 GB download
// doesn't pin a gigabyte of server RAM.
func (s *HTTPStaticServer) hFetch(w http.ResponseWriter, req *http.Request) {
	dirpath := s.getRealPath(req)

	// Auth — must have upload on the destination dir. Same
// body-preserving dance as hEdit: token from URL query / X-Token
// header, session fallback via canUploadSession.
	ac := s.readAccessConf(dirpath)
	token := req.URL.Query().Get("token")
	if token == "" {
		token = req.Header.Get("X-Token")
	}
	var allowed bool
	if token != "" {
		allowed = ac.canUploadByToken(token)
	} else {
		allowed = ac.canUploadSession(req)
	}
	if !allowed {
		http.Error(w, "Fetch forbidden", http.StatusForbidden)
		return
	}

	srcURL := req.FormValue("url")
	if srcURL == "" {
		http.Error(w, "Missing 'url' form field", http.StatusBadRequest)
		return
	}
	parsed, perr := url.Parse(srcURL)
	if perr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		http.Error(w, "Invalid URL — only http/https allowed", http.StatusBadRequest)
		return
	}
	if parsed.Host == "" {
		http.Error(w, "URL must have a host", http.StatusBadRequest)
		return
	}

	// Destination filename: must be a flat basename, no path
	// separators, must pass the existing filename character rules.
	dstName := req.FormValue("to")
	if dstName == "" {
		http.Error(w, "Missing 'to' form field", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(dstName, "/\\") {
		http.Error(w, "'to' must be a basename, not a path", http.StatusBadRequest)
		return
	}
	if err := checkFilename(dstName); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dstPath := filepath.Join(dirpath, filepath.FromSlash(dstName))

	// Make sure the parent dir exists (mirrors hUploadOrMkdir).
	if _, err := os.Stat(dirpath); os.IsNotExist(err) {
		if err := os.MkdirAll(dirpath, os.ModePerm); err != nil {
			http.Error(w, "Directory create "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Build an HTTP client with the safe dialer. We deliberately
	// don't follow redirects — a 30x to an internal address would
	// otherwise bypass the URL parse check.
	client := &http.Client{
		Timeout: fetchTimeout,
		// Disable redirects so the URL we validated is the URL
		// we connect to. A 30x to an internal address would otherwise
		// bypass the URL parse check; for /-/fetch we just fail loudly.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: safeDialContext,
		},
	}
	httpReq, _ := http.NewRequestWithContext(req.Context(), "GET", parsed.String(), nil)
	httpResp, err := client.Do(httpReq)
	if err != nil {
		http.Error(w, "Fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("Remote returned %d %s", httpResp.StatusCode, httpResp.Status), http.StatusBadGateway)
		return
	}

	dst, err := os.Create(dstPath)
	if err != nil {
		http.Error(w, "File create "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	buf := s.bufPool.Get().([]byte)
	defer s.bufPool.Put(buf)
	written, copyErr := io.CopyBuffer(dst, httpResp.Body, buf)
	if copyErr != nil {
		// Roll back: drop the partial file. The user can re-trigger.
		os.Remove(dstPath)
		http.Error(w, "Fetch copy failed: "+copyErr.Error(), http.StatusBadGateway)
		return
	}
	if written > maxFetchSize {
		os.Remove(dstPath)
		http.Error(w, fmt.Sprintf("Remote file too large (max %d bytes)", maxFetchSize), http.StatusRequestEntityTooLarge)
		return
	}

	invalidateDirSizeCache()

	w.Header().Set("Content-Type", "application/json;charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"success":     true,
		"destination": dstPath,
		"size":        written,
		"source":      parsed.String(),
	})
}

type FileJSONInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	Path    string `json:"path"`
	ModTime int64  `json:"mtime"`
	Extra   any    `json:"extra,omitempty"`
	// Md5 and Sha256 are hex-encoded digests. They are populated for
	// files only, and only when the file is at most maxHashSize —
	// hashing a multi-GB file over a slow disk is not worth the
	// request-blocking latency. Empty for directories or oversized
	// files.
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// maxHashSize caps how big a file can be when computing MD5/SHA on
// the fly inside hInfo. Bigger files just don't report hashes — the
// caller can still see size/mtime.
const maxHashSize int64 = 512 * 1024 * 1024 // 512 MiB

// computeFileHash streams the file once, updating an MD5 and a SHA256
// hasher in lock-step so we only walk the file a single time. Returns
// hex-encoded digests. io.CopyBuffer is used with a 32 KiB scratch
// buffer to match the rest of the server's stream-copy pattern.
func computeFileHash(path string) (md5sum, sha256sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	md5h := md5.New()
	sha256h := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		n, e := f.Read(buf)
		if n > 0 {
			md5h.Write(buf[:n])
			sha256h.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", "", e
		}
	}
	return hex.EncodeToString(md5h.Sum(nil)), hex.EncodeToString(sha256h.Sum(nil)), nil
}

// path should be absolute
func parseApkInfo(path string) (ai *ApkInfo) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("parse-apk-info panic:", err)
		}
	}()
	apkf, err := apk.OpenFile(path)
	if err != nil {
		return
	}
	ai = &ApkInfo{}
	ai.MainActivity, _ = apkf.MainActivity()
	ai.PackageName = apkf.PackageName()
	ai.Version.Code = int(apkf.Manifest().VersionCode.MustInt32())
	ai.Version.Name = apkf.Manifest().VersionName.MustString()
	return
}

func (s *HTTPStaticServer) hInfo(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	relPath := s.getRealPath(r)

	fi, err := os.Stat(relPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fji := &FileJSONInfo{
		Name:    fi.Name(),
		Size:    fi.Size(),
		Path:    path,
		ModTime: fi.ModTime().UnixNano() / 1e6,
	}
	ext := filepath.Ext(path)
	switch ext {
	case ".md":
		fji.Type = "markdown"
	case ".apk":
		fji.Type = "apk"
		fji.Extra = parseApkInfo(relPath)
	case ".ipa":
		// IPA metadata extraction was previously only wired into
		// hPlist (which builds the iPhone-install manifest). hInfo
		// returned a bare "text" record, so the file-info modal in
		// the frontend showed nothing useful for .ipa. parseIPA
		// returns nil + an error if the file isn't a valid IPA;
		// we degrade to type:"text" rather than 500-ing.
		fji.Type = "ipa"
		if plinfo, perr := parseIPA(relPath); perr == nil && plinfo != nil {
			fji.Extra = plinfo
		} else {
			fji.Extra = nil
		}
	case "":
		fji.Type = "dir"
	default:
		fji.Type = "text"
	}
	// Hash only files (not directories) and only when the file is
	// small enough that the request won't block for tens of seconds.
	// Errors here are non-fatal — size/mtime/path are still useful.
	if !fi.IsDir() && fi.Size() > 0 && fi.Size() <= maxHashSize {
		if md5sum, sha, herr := computeFileHash(relPath); herr == nil {
			fji.Md5 = md5sum
			fji.Sha256 = sha
		}
	}
	data, _ := json.Marshal(fji)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (s *HTTPStaticServer) hZip(w http.ResponseWriter, r *http.Request) {
	CompressToZip(w, s.getRealPath(r))
}

// zipMultiRequest is the body shape posted by the frontend's multi-select
// download. Paths are URL-decoded URL paths (the same shape as the rest of
// the API), and each one may be either a file or a directory.
type zipMultiRequest struct {
	Paths []string `json:"paths"`
}

// hZipMulti streams a single zip that packages every requested entry,
// using each entry's basename as the top-level name in the archive so the
// caller can unpack without knowing where each item came from. Missing or
// unreadable entries are silently skipped — the goal is "best effort
// download", not a strict transactional archive.
func (s *HTTPStaticServer) hZipMulti(w http.ResponseWriter, r *http.Request) {
	var req zipMultiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Paths) == 0 {
		http.Error(w, "No paths provided", http.StatusBadRequest)
		return
	}

	// Limit the request size to keep a single multi-download from pinning
	// the server: 64 KiB easily holds a few thousand URL paths.
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="download.zip"`)
	// Disable nginx response buffering so large archives start arriving
	// immediately rather than being held by an intermediate proxy.
	w.Header().Set("X-Accel-Buffering", "no")

	zw := &Zip{Writer: zip.NewWriter(w)}
	defer zw.Close()

	for _, p := range req.Paths {
		realPath := s.resolvePath(p)
		info, err := os.Stat(realPath)
		if err != nil {
			log.Printf("zip-multi skip %q: %v", realPath, err)
			continue
		}

		entryName := filepath.Base(realPath)
		if entryName == "" || entryName == "." || entryName == string(filepath.Separator) {
			continue
		}

		if info.IsDir() {
			dirName := entryName + "/"
			walkErr := filepath.Walk(realPath, func(path string, fi os.FileInfo, err error) error {
				if err != nil {
					// Skip entries we can't read rather than aborting
					// the whole archive.
					log.Printf("zip-multi walk skip %q: %v", path, err)
					return nil
				}
				if fi.Name() == YAMLCONF {
					return nil
				}
				rel, relErr := filepath.Rel(realPath, path)
				if relErr != nil {
					return nil
				}
				zipPath := dirName + filepath.ToSlash(rel)
				if fi.IsDir() {
					return zw.Add(zipPath+"/", path)
				}
				return zw.Add(zipPath, path)
			})
			if walkErr != nil {
				log.Printf("zip-multi walk %q: %v", realPath, walkErr)
			}
			continue
		}

		if err := zw.Add(entryName, realPath); err != nil {
			log.Printf("zip-multi add %q: %v", realPath, err)
		}
	}
}

func (s *HTTPStaticServer) hUnzip(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	zipPath, path := vars["zip_path"], vars["path"]
	ctype := mime.TypeByExtension(filepath.Ext(path))
	if ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	err := ExtractFromZip(filepath.Join(s.Root, zipPath), path, w)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
}

func combineURL(r *http.Request, path string) *url.URL {
	return &url.URL{
		Scheme: r.URL.Scheme,
		Host:   r.Host,
		Path:   path,
	}
}

func (s *HTTPStaticServer) hPlist(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	// rename *.plist to *.ipa
	if filepath.Ext(path) == ".plist" {
		path = path[0:len(path)-6] + ".ipa"
	}

	relPath := s.getRealPath(r)
	plinfo, err := parseIPA(relPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := &url.URL{
		Scheme: scheme,
		Host:   r.Host,
	}
	data, err := generateDownloadPlist(baseURL, path, plinfo)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/xml")
	w.Write(data)
}

func (s *HTTPStaticServer) hIpaLink(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	var plistUrl string

	if r.URL.Scheme == "https" {
		plistUrl = combineURL(r, "/-/ipa/plist/"+path).String()
	} else if s.PlistProxy != "" {
		httpPlistLink := "http://" + r.Host + "/-/ipa/plist/" + path
		url, err := s.genPlistLink(httpPlistLink)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		plistUrl = url
	} else {
		http.Error(w, "500: Server should be https:// or provide valid plistproxy", 500)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	log.Println("PlistURL:", plistUrl)
	renderHTML(w, "ipa-install", ipaInstallHTML, map[string]string{
		"Name":      filepath.Base(path),
		"PlistLink": plistUrl,
	})
}

func (s *HTTPStaticServer) genPlistLink(httpPlistLink string) (plistUrl string, err error) {
	// Maybe need a proxy, a little slowly now.
	pp := s.PlistProxy
	if pp == "" {
		pp = defaultPlistProxy
	}
	resp, err := http.Get(httpPlistLink)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	retData, err := http.Post(pp, "text/xml", bytes.NewBuffer(data))
	if err != nil {
		return
	}
	defer retData.Body.Close()

	jsonData, _ := io.ReadAll(retData.Body)
	var ret map[string]string
	if err = json.Unmarshal(jsonData, &ret); err != nil {
		return
	}
	plistUrl = pp + "/" + ret["key"]
	return
}

func (s *HTTPStaticServer) hFileOrDirectory(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.getRealPath(r))
}

type HTTPFileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
}

type AccessTable struct {
	Regex string `yaml:"regex"`
	Allow bool   `yaml:"allow"`
}

type UserControl struct {
	Email string
	// Access bool
	Upload bool
	Delete bool
	Token  string
}

type AccessConf struct {
	Upload       bool          `yaml:"upload" json:"upload"`
	Delete       bool          `yaml:"delete" json:"delete"`
	Users        []UserControl `yaml:"users" json:"users"`
	AccessTables []AccessTable `yaml:"accessTables"`
}

var reCache = make(map[string]*regexp.Regexp)

func (c *AccessConf) canAccess(fileName string) bool {
	for _, table := range c.AccessTables {
		pattern, ok := reCache[table.Regex]
		if !ok {
			pattern, _ = regexp.Compile(table.Regex)
			reCache[table.Regex] = pattern
		}
		// skip wrong format regex
		if pattern == nil {
			continue
		}
		if pattern.MatchString(fileName) {
			return table.Allow
		}
	}
	return true
}

func (c *AccessConf) canDelete(r *http.Request) bool {
	session, err := store.Get(r, defaultSessionName)
	if err != nil {
		return c.Delete
	}
	val := session.Values["user"]
	if val == nil {
		return c.Delete
	}
	userInfo := val.(*UserInfo)
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Delete
		}
	}
	return c.Delete
}

func (c *AccessConf) canUploadByToken(token string) bool {
	for _, rule := range c.Users {
		if rule.Token == token {
			return rule.Upload
		}
	}
	return c.Upload
}

// canUploadSession is the session-based half of canUpload, factored
// out so PUT-style handlers (whose body IS the file content) can do
// auth without draining the body via r.FormValue. Token auth is the
// path callers should use; this is the fallback for browser session
// login.
func (c *AccessConf) canUploadSession(r *http.Request) bool {
	session, err := store.Get(r, defaultSessionName)
	if err != nil {
		return c.Upload
	}
	val := session.Values["user"]
	if val == nil {
		return c.Upload
	}
	userInfo := val.(*UserInfo)
	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Upload
		}
	}
	return c.Upload
}

func (c *AccessConf) canUpload(r *http.Request) bool {
	token := r.FormValue("token")
	if token != "" {
		return c.canUploadByToken(token)
	}
	session, err := store.Get(r, defaultSessionName)
	if err != nil {
		return c.Upload
	}
	val := session.Values["user"]
	if val == nil {
		return c.Upload
	}
	userInfo := val.(*UserInfo)

	for _, rule := range c.Users {
		if rule.Email == userInfo.Email {
			return rule.Upload
		}
	}
	return c.Upload
}

func (s *HTTPStaticServer) hJSONList(w http.ResponseWriter, r *http.Request) {
	requestPath := mux.Vars(r)["path"]
	realPath := s.getRealPath(r)
	search := r.FormValue("search")
	auth := s.readAccessConf(realPath)
	auth.Upload = auth.canUpload(r)
	auth.Delete = auth.canDelete(r)
	maxDepth := s.DeepPathMaxDepth

	// path string -> info os.FileInfo
	fileInfoMap := make(map[string]os.FileInfo, 0)

	if search != "" {
		results := s.findIndex(search)
		if len(results) > 50 { // max 50
			results = results[:50]
		}
		for _, item := range results {
			if filepath.HasPrefix(item.Path, requestPath) {
				fileInfoMap[item.Path] = item.Info
			}
		}
	} else {
		entries, err := os.ReadDir(realPath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			fileInfoMap[filepath.Join(requestPath, entry.Name())] = info
		}
	}

	// turn file list -> json
	lrs := make([]HTTPFileInfo, 0)
	for path, info := range fileInfoMap {
		if !auth.canAccess(info.Name()) {
			continue
		}
		lr := HTTPFileInfo{
			Name:    info.Name(),
			Path:    path,
			ModTime: info.ModTime().UnixNano() / 1e6,
		}
		if search != "" {
			name, err := filepath.Rel(requestPath, path)
			if err != nil {
				log.Println(requestPath, path, err)
			}
			lr.Name = filepath.ToSlash(name) // fix for windows
		}
		if info.IsDir() {
			name := deepPath(realPath, info.Name(), maxDepth)
			lr.Name = name
			lr.Path = filepath.Join(filepath.Dir(path), name)
			lr.Type = "dir"
			lr.Size = s.historyDirSize(lr.Path)
		} else {
			lr.Type = "file"
			lr.Size = info.Size() // formatSize(info)
		}
		lrs = append(lrs, lr)
	}

	// Sort the output by name before marshalling. The upstream
	// collection uses a Go map (fileInfoMap) whose iteration order
	// is intentionally randomised to mitigate hash-flood attacks, so
	// the raw JSON would otherwise come back in a different order
	// on every refresh — and even though the frontend re-sorts by
	// mtime, items that share a mtime (common for files uploaded in
	// the same second, or files copied together) would inherit that
	// random order via the JS stable-sort tiebreaker. Sorting here
	// gives every consumer of this API a stable, predictable
	// baseline order; the frontend sort then layers on top of it.
	sort.Slice(lrs, func(i, j int) bool {
		return lrs[i].Name < lrs[j].Name
	})

	data, _ := json.Marshal(map[string]any{
		"files": lrs,
		"auth":  auth,
	})
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

var dirInfoSize = Directory{size: make(map[string]int64), mutex: &sync.RWMutex{}}

func (s *HTTPStaticServer) makeIndex() error {
	var indexes = make([]IndexFileItem, 0)
	var err = filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("WARN: Visit path: %s error: %v", strconv.Quote(path), err)
			return filepath.SkipDir
			// return err
		}
		if info.IsDir() {
			return nil
		}

		path, _ = filepath.Rel(s.Root, path)
		path = filepath.ToSlash(path)
		indexes = append(indexes, IndexFileItem{path, info})
		return nil
	})
	s.indexes = indexes
	// Drop the directory-size cache so the next read recomputes against
	// the freshly-walked index. Without this every displayed directory
	// size stays pinned to whatever the very first walk observed —
	// uploads, deletes, and edits are invisible until the server
	// restarts. Cheap to do: the cache rebuilds lazily on demand.
	dirInfoSize.mutex.Lock()
	dirInfoSize.size = make(map[string]int64)
	dirInfoSize.mutex.Unlock()
	return err
}

func (s *HTTPStaticServer) historyDirSize(dir string) int64 {
	// Normalise to forward slashes so the cache key matches what the
	// invalidation paths write (also ToSlash'd).
	dir = filepath.ToSlash(filepath.Clean(dir))

	dirInfoSize.mutex.RLock()
	size, ok := dirInfoSize.size[dir]
	dirInfoSize.mutex.RUnlock()

	if ok {
		return size
	}

	// Walk the actual filesystem rather than relying on s.indexes. The
	// index is rebuilt by makeIndex every 10 minutes, so reading from
	// it would leave a freshly-uploaded (or extracted) directory
	// reporting a stale size until the next cycle. Walking the real
	// tree is O(n) in the directory's file count, which is acceptable
	// for a file manager and gives the user an immediately-correct
	// number after upload/unzip/delete.
	absDir := filepath.Join(s.Root, dir)
	filepath.Walk(absDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		size += info.Size()
		return nil
	})

	dirInfoSize.mutex.Lock()
	dirInfoSize.size[dir] = size
	dirInfoSize.mutex.Unlock()

	return size
}

// invalidateDirSizeCache drops every cached size entry. Called after any
// mutation (upload, unzip, delete) so the next read computes fresh
// against the real filesystem. Cheap: the map rebuilds lazily on demand.
func invalidateDirSizeCache() {
	dirInfoSize.mutex.Lock()
	dirInfoSize.size = make(map[string]int64)
	dirInfoSize.mutex.Unlock()
}

func (s *HTTPStaticServer) findIndex(text string) []IndexFileItem {
	ret := make([]IndexFileItem, 0)
	for _, item := range s.indexes {
		ok := true
		// search algorithm, space for AND
		for keyword := range strings.FieldsSeq(text) {
			needContains := true
			if strings.HasPrefix(keyword, "-") {
				needContains = false
				keyword = keyword[1:]
			}
			if keyword == "" {
				continue
			}
			ok = (needContains == strings.Contains(strings.ToLower(item.Path), strings.ToLower(keyword)))
			if !ok {
				break
			}
		}
		if ok {
			ret = append(ret, item)
		}
	}
	return ret
}

func (s *HTTPStaticServer) defaultAccessConf() AccessConf {
	return AccessConf{
		Upload: s.Upload,
		Delete: s.Delete,
	}
}

func (s *HTTPStaticServer) readAccessConf(realPath string) (ac AccessConf) {
	relativePath, err := filepath.Rel(s.Root, realPath)
	if err != nil || relativePath == "." || relativePath == "" { // actually relativePath is always "." if root == realPath
		ac = s.defaultAccessConf()
		realPath = s.Root
	} else {
		parentPath := filepath.Dir(realPath)
		ac = s.readAccessConf(parentPath)
	}
	if isFile(realPath) {
		realPath = filepath.Dir(realPath)
	}
	cfgFile := filepath.Join(realPath, YAMLCONF)
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("Err read .ghs.yml: %v", err)
	}
	err = yaml.Unmarshal(data, &ac)
	if err != nil {
		log.Printf("Err format .ghs.yml: %v", err)
	}
	return
}

func deepPath(basedir, name string, maxDepth int) string {
	// loop max 5, incase of for loop not finished
	for depth := 0; depth <= maxDepth; depth += 1 {
		entries, err := os.ReadDir(filepath.Join(basedir, name))
		if err != nil || len(entries) != 1 {
			break
		}
		if entries[0].IsDir() {
			name = filepath.ToSlash(filepath.Join(name, entries[0].Name()))
		} else {
			break
		}
	}
	return name
}

const ipaInstallHTML = `<!DOCTYPE html>
<html>
<head>
  <title>[[.Name]] install</title>
  <meta http-equiv="Content-Type" content="text/HTML; charset=utf-8">
  <meta content="target-densitydpi=device-dpi,width=640" name="viewport" id="viewport">
  <script>
    function showById(name) {
      document.getElementById(name).style.display = 'block';
    }
    function checkBrowerAndDownload() {
      var ua = navigator.userAgent.toLowerCase();
      var isIOS = /iphone|ipad|ipod/.test(ua);
      var isAndroid = /android/.test(ua);
      var isWechat = /micromessenger/.test(ua);
      var plistLink = "[[.PlistLink]]";
      var ipaInstallLink = 'itms-services://?action=download-manifest&url=' + plistLink;
      document.getElementById('itms-link').href = ipaInstallLink;
      if (isWechat) {
        showById('safari');
        location.href = ipaInstallLink;
      } else if (isAndroid) {
        showById('android');
      } else if (isIOS) {
        showById('safari');
        location.href = ipaInstallLink;
      } else {
        showById('browser');
      }
    }
  </script>
</head>
<body>
  <div id="browser" style="display:none">
    This is IPA install page, open this link with your iPhone.
  </div>
  <div id="safari" style="display:none">
    If install not started soon, click <a id="itms-link" href="#">here</a>
  </div>
  <div id="android" style="display:none">
    This is IPA install page, not for android.
  </div>
  <script>checkBrowerAndDownload();</script>
</body>
</html>`

const videoPlayerHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Video Player - [[.FileName]]</title>
    <style>
        body, html { margin:0; padding:0; height:100%; width:100%; overflow:hidden; background-color:#000; }
        .video-container { display:flex; flex-direction:column; justify-content:center; align-items:center; height:100%; }
        video { max-width:100%; max-height:100%; }
        h1 { color:#fff; font-size:24px; margin-bottom:20px; }
    </style>
</head>
<body>
    <div class="video-container">
        <video id="videoPlayer" controls autoplay>
            <source src="[[.VideoURL]]" type="video/[[.Extension]]">
            Your browser does not support the video tag.
        </video>
    </div>
    <script>
        document.addEventListener('DOMContentLoaded', function() {
            document.getElementById('videoPlayer').focus();
        });
    </script>
</body>
</html>`

var funcMap = template.FuncMap{
	"title": strings.Title,
}

var _tmpls = make(map[string]*template.Template)

func renderHTML(w http.ResponseWriter, name, content string, v any) {
	if t, ok := _tmpls[name]; ok {
		t.Execute(w, v)
		return
	}
	t := template.Must(template.New(name).Funcs(funcMap).Delims("[[", "]]").Parse(content))
	_tmpls[name] = t
	t.Execute(w, v)
}

func checkFilename(name string) error {
	if strings.ContainsAny(name, "\\/:*<>|") {
		return errors.New("Name should not contains \\/:*<>|")
	}
	return nil
}

// checkPathSegment is the per-segment variant of checkFilename used when
// validating a relative path (e.g. "MyFolder/sub/foo.txt"). The caller
// is expected to split on "/" first, so the path-separator rule is
// dropped; the rest of the forbid-list stays. Empty, "." and ".."
// segments are rejected so callers can't smuggle a parent-dir escape
// past the split.
func checkPathSegment(seg string) error {
	if seg == "" || seg == "." || seg == ".." {
		return errors.New("Invalid path segment")
	}
	if strings.ContainsAny(seg, "\\:*<>|\"\x00") {
		return errors.New("Path segment should not contain \\:*<>|\"")
	}
	return nil
}

func (s *HTTPStaticServer) hVideoPlayer(w http.ResponseWriter, r *http.Request) {
	path := mux.Vars(r)["path"]
	realPath := s.getRealPath(r)
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))

	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	fileName := filepath.Base(path)

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	videoURL := fmt.Sprintf("%s://%s/%s", scheme, r.Host, path)

	renderHTML(w, "video-player", videoPlayerHTML, map[string]any{
		"FileName":  fileName,
		"VideoURL":  videoURL,
		"Extension": extension,
	})
}
