package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// loginProviderName is the discriminator used in session.Values["user"] to
// distinguish a username/password login from OpenID or oauth2-proxy logins.
// The frontend uses this to decide whether to show the "change password"
// entry in the user menu.
const loginProviderName = "login"

func init() {
	// gorilla/sessions stores session values via gob. The interface
	// type stored under "user" needs to be enumerated here, otherwise
	// session.Save fails with "type not registered for interface" the
	// first time someone authenticates.
	gob.Register(&LoginUser{})
}

// LoginUser is the session value stored when a user logs in via the
// built-in --login flow. Different from UserInfo (which is set by OpenID
// / OAuth2) — the Provider field tells the rest of the app how the user
// authenticated, and keeps the type registration in gor/sessions forward-
// compatible.
type LoginUser struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// loginCredentials is the on-disk shape of auth-state.json. The file is
// intentionally a tiny, hand-written struct (no schema versioning) so
// operators can `cat` it. Atomicity on save is provided by write-to-temp
// + os.Rename below.
type loginCredentials struct {
	Username      string `json:"username"`
	PasswordSHA   string `json:"password_sha256"` // hex SHA-256
	CreatedAtUnix int64  `json:"created_at"`
	UpdatedAtUnix int64  `json:"updated_at"`
}

// loginState bundles the in-memory credentials with a mutex guarding
// concurrent load/save. Loads are snapshot reads, saves are exclusive
// writes — the actual on-disk file is updated atomically (write + fsync
// + rename) so a crash mid-save can't leave a half-written JSON.
type loginState struct {
	mu    sync.RWMutex
	creds *loginCredentials
	path  string
}

// load reads auth-state.json (or initialises defaults if missing), and
// returns the in-memory state. Errors during read are logged but not
// fatal — the server falls back to the default admin/admin credentials so
// a misconfigured operator never locks themselves out of their own box.
//
// When the file is absent the defaults are kept in memory ONLY — no file
// is written. This keeps transient runs (e.g. `gohttpserver --login` for
// a quick test) from littering auth-state.json across the filesystem.
// The file is created on the first successful changePassword call, which
// is the moment the operator has actually chosen a secret worth keeping.
func loadLoginCredentials(path string) *loginState {
	st := &loginState{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("login: failed to read %s (%v); using default credentials", path, err)
		}
		st.creds = defaultLoginCredentials()
		log.Printf("login: no %s found; using in-memory default credentials (admin/admin). " +
			"Change the password via PUT /-/api/auth/password to persist a new one.", path)
		return st
	}

	var c loginCredentials
	if err := json.Unmarshal(data, &c); err != nil {
		log.Printf("login: %s is not valid JSON (%v); using default credentials", path, err)
		c = *defaultLoginCredentials()
	}
	if c.Username == "" || c.PasswordSHA == "" {
		log.Printf("login: %s missing username or password; using default credentials", path)
		c = *defaultLoginCredentials()
	}
	st.creds = &c
	return st
}

// defaultLoginCredentials returns admin/admin (SHA-256). Hex string is
// computed once here so callers don't have to rebuild it on every
// fallback path.
func defaultLoginCredentials() *loginCredentials {
	now := time.Now().Unix()
	return &loginCredentials{
		Username:      "admin",
		PasswordSHA:   sha256Hex("admin"),
		CreatedAtUnix: now,
		UpdatedAtUnix: now,
	}
}

// sha256Hex returns the hex-encoded SHA-256 of s.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// verify returns true iff username matches the stored username AND the
// SHA-256 of password matches the stored hash. Both comparisons go
// through subtle.ConstantTimeCompare so timing-based enumeration of
// either field is impractical against the localhost file manager.
func (s *loginState) verify(username, password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds == nil {
		return false
	}
	wantUser := sha256Hex(username)
	wantPass := sha256Hex(password)
	userMatch := subtle.ConstantTimeCompare([]byte(wantUser), []byte(sha256Hex(s.creds.Username))) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(wantPass), []byte(s.creds.PasswordSHA)) == 1
	return userMatch && passMatch
}

// changePassword verifies the old password, then atomically replaces the
// stored hash. Returns an error if old is wrong or if the file write
// fails. The lock is held across saveLogin so two concurrent changes
// can't race and lose one of the writes.
func (s *loginState) changePassword(old, new string) error {
	if !s.verify(s.credsUsername(), old) {
		return errors.New("invalid current password")
	}
	if len(new) < 4 {
		return errors.New("new password must be at least 4 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creds.PasswordSHA = sha256Hex(new)
	s.creds.UpdatedAtUnix = time.Now().Unix()
	return s.saveLocked()
}

// changeUsername updates the stored username after the caller has
// already verified the current password (the admin API handler
// re-authenticates before calling this). We don't take the password
// again here — the handler does the verify, then calls us under the
// assumption the rename is authorised.
//
// oldName is passed in so we can assert we're renaming the right user
// (defensive — if the session and the on-disk state have diverged, we
// refuse rather than silently renaming the wrong account).
func (s *loginState) changeUsername(oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.creds == nil {
		return errors.New("login: no credentials loaded")
	}
	if s.creds.Username != oldName {
		return errors.New("login: username changed concurrently; refusing rename")
	}
	s.creds.Username = newName
	s.creds.UpdatedAtUnix = time.Now().Unix()
	return s.saveLocked()
}

// credsUsername returns the stored username under the read lock.
func (s *loginState) credsUsername() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.creds == nil {
		return "admin"
	}
	return s.creds.Username
}

// save writes the credentials to disk atomically. Callers must hold
// s.mu in write mode (via saveLocked) — the public save() helper below
// does the right locking.
func (s *loginState) saveLocked() error {
	if s.creds == nil {
		return errors.New("login: no credentials loaded")
	}
	data, err := json.MarshalIndent(s.creds, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return mkErr
		}
	}
	tmp, err := os.CreateTemp(dir, "auth-state-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		// If we never renamed, clean up the temp file.
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

// save is the locking wrapper around saveLocked; used during initial
// load to persist defaulted credentials.
func (s *loginState) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

// ─── Routes / middleware ────────────────────────────────────────────────

// loginWhitelist lists every path that bypasses authentication when --login
// is enabled. Anything not matched falls through to the login gate.
//
// Static assets (/-/frontend/*) are deliberately white-listed so the
// login SPA can be served without making the user sign in first.
// /-/user, /-/sysinfo, /-/api/auth/status and /-/api/auth/password are
// white-listed because:
//   - The SPA calls /-/user and /-/sysinfo at startup; gating them
//     would mean the SPA can't even mount.
//   - /-/api/auth/status tells the SPA whether --login is on so it
//     knows to render the login page.
//
// The actual auth check then happens *inside* /-/api/auth/password (via
// session lookup), so a session-less request to change the password is
// rejected with 401.
var loginWhitelist = []string{
	"/-/login",
	"/-/logout",
	"/-/user",
	"/-/sysinfo",
	"/-/api/auth/status",
	"/-/api/auth/password",
	"/-/frontend/",
	"/favicon.ico",
	"/favicon.png",
	// WebDAV has its own HTTP Basic Auth (independent of the session
	// cookie) — gating /dav/ behind the login middleware would lock
	// out WebDAV clients that don't share cookies with the browser.
	// The webdav handler itself rejects requests that fail its own
	// account/password verification.
	"/dav/",
}

// isLoginWhitelisted returns true if the request path is a path that
// must remain reachable without auth. The middleware check below uses
// path prefix matches against this list.
func isLoginWhitelisted(path string) bool {
	// Exact-match the WebDAV collection without a trailing slash — some
	// clients (GNOME/gvfs on Linux) probe "/dav" during discovery. This
	// is an exact check so unrelated "/davXXX" paths are NOT whitelisted;
	// the "/dav/" prefix in the list below covers everything under it.
	if path == "/dav" {
		return true
	}
	for _, p := range loginWhitelist {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// loginWantsJSON returns true for requests the middleware should answer
// with 401 JSON instead of a 302 HTML redirect. The rule: anything that
// isn't a top-level navigation in a browser — fetch / XHR / curl /
// range probes — gets JSON.
//
// We treat Accept containing "text/html" alone as a browser navigation;
// fetch() requests with Accept: */* or application/json get JSON.
func loginWantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		return true
	}
	// fetch() typically sends Accept: */* with no text/html. Treat any
	// non-text/html Accept as a programmatic request.
	if accept != "" && !strings.Contains(accept, "text/html") {
		return true
	}
	// Crawlers and direct link paste: no Accept at all → treat as JSON
	// 401 so an opaque "you need to log in" doesn't leak the file
	// listing or directory path.
	if accept == "" {
		return true
	}
	return false
}

// loginAuthMiddleware gates every request behind a session check when
// --login is enabled. The wrapped handler is only reached once we've
// verified the request carries a valid session carrying a *LoginUser.
//
// Design note: the wrapper is plain http.HandlerFunc wrapping rather
// than gorilla/mux middleware. The mux.NewRouter() in main() applies
// auth via wrapping hdlr (which is what gets mounted under
// /). Putting this gate in front of hdlr means EVERY route — file
// handler, /-/upload, /-/delete, /-/info, … — sees the same check,
// instead of having to thread it into every handler individually.
func loginAuthMiddleware(loginState *loginState, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Strip the configured URL prefix before comparing to the whitelist
		// so that --prefix /foo doesn't accidentally re-gate /-/login.
		checkPath := strings.TrimPrefix(r.URL.Path, gcfg.Prefix)
		if isLoginWhitelisted(checkPath) {
			next.ServeHTTP(w, r)
			return
		}

		if userFromLoginSession(r) != nil {
			next.ServeHTTP(w, r)
			return
		}

		// Not authenticated. Redirect browser navigations to the login
		// page; answer everything else with 401 JSON so fetch() callers
		// get a parseable error and don't end up with the login HTML in
		// their response body.
		if loginWantsJSON(r) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized","login_required":true}`))
			return
		}

		nextURL := safeNextURL(r, r.URL.RequestURI())
		loginURL := "/-/login?next=" + url.QueryEscape(nextURL)
		// If we're mounted under a prefix, the login page lives under
		// the same prefix; the browser follows the relative redirect.
		http.Redirect(w, r, loginURL, http.StatusFound)
	})
}

// userFromLoginSession pulls the *LoginUser out of the session cookie,
// returning nil for unauthenticated / forged / foreign-session requests.
// We deliberately do NOT widen the existing userFromSession helper
// because that helper checks for *UserInfo (the OpenID type), and
// treating both interchangeably would mean a session whose "user" was
// set by OpenID would silently satisfy login-only routes.
func userFromLoginSession(r *http.Request) *LoginUser {
	// store.Get returns a usable session even when the existing cookie
	// can't be decoded (e.g. stale cookie from a previous server run
	// with a different session key). If the decode failed, the session
	// simply has no values — so we fall through to the nil-check below
	// and return nil, which is the correct "not logged in" result.
	// We must NOT return nil on the error itself, because the error
	// doesn't mean "no session" — it means "couldn't read the old one",
	// and the returned session is still safe to inspect.
	session, _ := store.Get(r, defaultSessionName)
	val := session.Values["user"]
	if val == nil {
		return nil
	}
	u, ok := val.(*LoginUser)
	if !ok {
		return nil
	}
	return u
}

// handleLoginUI serves the SPA's index.html for GET /-/login.
//
// Previously this rendered a standalone HTML login form. That created
// TWO different login UIs — the backend HTML form and the SPA's
// Login.vue — which confused users who saw different-looking login
// pages before and after authentication. Now we always serve the SPA
// so there's exactly one login UI (Login.vue). The SPA reads the
// `next` and `error` query params to pre-fill its state.
//
// If the request already carries a valid session (e.g. the user
// navigated to /-/login manually while signed in), redirect to "/"
// instead of showing the login page again.
func handleLoginUI(w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	data, err := frontendDistFS.ReadFile("frontend/dist/index.html")
	if err != nil {
		http.Error(w, "login page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// handleLoginSubmit validates username/password against the stored
// credentials and, on success, stamps the session with a *LoginUser
// before redirecting to ?next=.
//
// Wrong credentials re-render the login form with an error rather than
// 401, so the browser stays inside the login flow and the URL bar still
// matches what the user expects.
func handleLoginSubmit(state *loginState, w http.ResponseWriter, r *http.Request) {
	// ParseMultipartForm handles BOTH multipart/form-data (sent by the
	// browser's FormData API, which the SPA's submitLogin uses) and
	// application/x-www-form-urlencoded (sent by plain HTML forms). It
	// calls ParseForm() internally. We must NOT call ParseForm() first —
	// doing so populates r.Form with only the query string, and then
	// FormValue() skips the multipart parse, leaving all fields empty.
	if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	next := safeNextURL(r, r.FormValue("next"))

	if username == "" || password == "" {
		redirectToLoginWithError(w, r, next, "missing_credentials")
		return
	}

	if !state.verify(username, password) {
		redirectToLoginWithError(w, r, next, "invalid_credentials")
		return
	}

	// store.Get returns a usable (new, empty) session even when the
	// existing cookie can't be decoded — e.g. after a server restart
	// with a random session key, or a corrupted/tampered cookie. The
	// returned error just means "I couldn't read the old session";
	// the session itself is perfectly writable. Returning 500 here
	// would lock users out whenever they carry a stale cookie, so we
	// ignore the error and stamp the fresh session.
	session, _ := store.Get(r, defaultSessionName)
	session.Values["user"] = &LoginUser{
		Name:     username,
		Provider: loginProviderName,
	}
	if err := session.Save(r, w); err != nil {
		log.Printf("login: session save failed: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func redirectToLoginWithError(w http.ResponseWriter, r *http.Request, next, code string) {
	q := url.Values{}
	q.Set("next", next)
	q.Set("error", code)
	http.Redirect(w, r, "/-/login?"+q.Encode(), http.StatusFound)
}

// handleLoginLogout clears the session's "user" entry and redirects to
// ?next= (or "/"). Idempotent: calling logout without an active
// session is a no-op redirect.
func handleLoginLogout(w http.ResponseWriter, r *http.Request) {
	session, err := store.Get(r, defaultSessionName)
	if err == nil {
		delete(session.Values, "user")
		session.Options.MaxAge = -1
		_ = session.Save(r, w)
	}
	next := safeNextURL(r, r.FormValue("next"))
	http.Redirect(w, r, next, http.StatusFound)
}

// handleAuthStatus returns a tiny JSON body describing whether login is
// enabled and (if so) whether the current request is authenticated. The
// frontend uses this to decide between rendering the file manager or
// redirecting to the login page at startup.
//
// We expose this endpoint (instead of computing login_required purely
// from /-/user) so the SPA can tell the difference between "auth not
// configured" and "auth configured but you're not signed in" — both
// cases look the same (user == null) under /-/user alone.
func handleAuthStatus(loginEnabled bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		user := userFromLoginSession(r)
		status := map[string]any{
			"login_enabled": loginEnabled,
		}
		if loginEnabled && user != nil {
			status["authenticated"] = true
			status["name"] = user.Name
		} else if loginEnabled {
			status["authenticated"] = false
		}
		_ = json.NewEncoder(w).Encode(status)
	})
}

// handleChangePassword accepts JSON {old, new} and, if the caller is
// authenticated AND old matches, persists new. Authenticated here
// means "logged in via --login" — we re-check via userFromLoginSession
// so an old OpenID cookie can't be used to rotate the local password.
func handleChangePassword(state *loginState, w http.ResponseWriter, r *http.Request) {
	if userFromLoginSession(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		http.Error(w, "Body too large or unreadable", http.StatusBadRequest)
		return
	}
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if err := state.changePassword(req.Old, req.New); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"success":true}`))
}

// registerLoginRoutes wires the --login endpoints into a router. The
// router passed in should be the post-prefix subrouter so --prefix works.
// /-/login MUST be reachable *before* the loginAuthMiddleware, so
// registerLoginRoutes also returns a flag set of internal paths the
// caller must NOT include in the gated handler chain.
//
// mux doesn't easily let you exclude paths from a handler, so the
// caller must mount the gated handler on the / fallback route AFTER
// these specific routes are registered.
func registerLoginRoutes(router *mux.Router, state *loginState, loginEnabled bool) {
	router.HandleFunc("/-/login", handleLoginUI).Methods("GET")
	// Wrapper to bind state without dragging the parameter through
	// every callsite.
	router.HandleFunc("/-/login", func(w http.ResponseWriter, r *http.Request) {
		handleLoginSubmit(state, w, r)
	}).Methods("POST")

	router.HandleFunc("/-/logout", handleLoginLogout).Methods("GET")
	router.HandleFunc("/-/api/auth/status", handleAuthStatus(loginEnabled).ServeHTTP).Methods("GET")
	router.HandleFunc("/-/api/auth/password", func(w http.ResponseWriter, r *http.Request) {
		handleChangePassword(state, w, r)
	}).Methods("PUT")
}
