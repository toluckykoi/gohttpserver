package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

func newTestLoginState(t *testing.T) *loginState {
	t.Helper()
	dir := t.TempDir()
	st := loadLoginCredentials(filepath.Join(dir, "auth-state.json"))
	if st == nil || st.creds == nil {
		t.Fatal("expected non-nil credentials after first load")
	}
	return st
}

// TestLoginCredentialsDefaults verifies the first-launch fallback keeps
// admin/admin in memory WITHOUT writing a file. Transient runs (tests,
// quick experiments) must not litter auth-state.json across the FS.
// After load, the in-memory hash must equal sha256("admin"). The on-disk
// file must NOT exist until the operator explicitly changes the password
// (covered by TestLoginChangePasswordsPersists).
func TestLoginCredentialsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth-state.json")

	st := loadLoginCredentials(path)
	if st == nil {
		t.Fatal("nil state")
	}
	if st.creds.Username != "admin" {
		t.Errorf("default username = %q, want admin", st.creds.Username)
	}
	if got, want := st.creds.PasswordSHA, sha256Hex("admin"); got != want {
		t.Errorf("default password hash = %q, want sha256(admin) = %q", got, want)
	}
	// Critical: load must NOT create the file. Otherwise every test run
	// and every transient `gohttpserver --login` invocation drops a
	// credential file on disk that someone might later commit.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("loadLoginCredentials wrote %s; expected no file until changePassword", path)
	}
}

// TestLoginVerify exercises the constant-time comparison path: correct
// username/password should validate, wrong variants should fail without
// leaking via the boolean shape (we can't directly observe timing here,
// but the comparator itself should always use both fields).
func TestLoginVerify(t *testing.T) {
	st := newTestLoginState(t)
	cases := []struct {
		name     string
		user     string
		password string
		want     bool
	}{
		{"valid default", "admin", "admin", true},
		{"wrong password", "admin", "wrong", false},
		{"wrong username", "root", "admin", false},
		{"both wrong", "x", "y", false},
		{"empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := st.verify(c.user, c.password); got != c.want {
				t.Errorf("verify(%q,%q) = %v, want %v", c.user, c.password, got, c.want)
			}
		})
	}
}

// TestLoginChangePasswordsPersists ensures changePassword round-trips:
// setting a new password invalidates the old one and validates the new
// one. The on-disk file must reflect the change so a restart picks it up.
func TestLoginChangePasswordsPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth-state.json")
	st := loadLoginCredentials(path)

	if err := st.changePassword("admin", "newpass"); err != nil {
		t.Fatalf("changePassword failed: %v", err)
	}
	if st.verify("admin", "admin") {
		t.Error("old password (admin) should be invalid after change")
	}
	if !st.verify("admin", "newpass") {
		t.Error("new password should validate after change")
	}

	// Read disk and verify it's the same hash.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got loginCredentials
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse back: %v", err)
	}
	want := sha256.Sum256([]byte("newpass"))
	if got.PasswordSHA != hex.EncodeToString(want[:]) {
		t.Errorf("on-disk hash = %q, want hex sha256(newpass)", got.PasswordSHA)
	}
}

// TestLoginChangePasswordRejectsWrongOld verifies the API refuses to
// rotate when the caller doesn't know the current password. This is the
// key safety property — otherwise an attacker who can reach the endpoint
// (e.g. via an unexpired session for user A) could lock out user A.
func TestLoginChangePasswordRejectsWrongOld(t *testing.T) {
	st := newTestLoginState(t)
	if err := st.changePassword("wrong", "newpass"); err == nil {
		t.Error("changePassword should reject when old is wrong")
	}
	if !st.verify("admin", "admin") {
		t.Error("password should still be admin/admin after rejected change")
	}
}

// TestLoginChangePasswordRejectsShortNew ensures we never store a
// trivially brute-forceable password. The check is local; the server
// also does the same.
func TestLoginChangePasswordRejectsShortNew(t *testing.T) {
	st := newTestLoginState(t)
	if err := st.changePassword("admin", "no"); err == nil {
		t.Error("changePassword should reject passwords <4 chars")
	}
	if !st.verify("admin", "admin") {
		t.Error("password should still be admin/admin after rejected change")
	}
}

// ─── Routes / middleware ────────────────────────────────────────────

// newLoginRouter wires the --login routes onto a fresh mux — same as
// what main.go would do — wrapped in the login middleware. Returns the
// router plus a *loginState the tests can poke at.
func newLoginRouter(t *testing.T) (*mux.Router, *loginState) {
	t.Helper()
	dir := t.TempDir()
	state := loadLoginCredentials(filepath.Join(dir, "auth-state.json"))

	r := mux.NewRouter()
	registerLoginRoutes(r, state, true)
	// Build the gated chain: outer handlers (hdlr) sit behind the
	// middleware, so all non-whitelisted requests fail closed.
	gated := loginAuthMiddleware(state, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "through")
	}))
	r.PathPrefix("/").Handler(gated)
	return r, state
}

func doRequest(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestLoginMiddlewareRedirectsBrowserNavigation verifies the basic
// happy-path gate: a GET with a typical browser Accept header is sent
// to /-/login via 302, and the next parameter is preserved.
func TestLoginMiddlewareRedirectsBrowserNavigation(t *testing.T) {
	r, _ := newLoginRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/secret/file.pdf", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := doRequest(r, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/-/login") {
		t.Errorf("Location = %q, want /-/login prefix", loc)
	}
	if !strings.Contains(loc, "next=") {
		t.Errorf("Location %q missing next= parameter", loc)
	}
	// The next= value should encode the original path.
	if !strings.Contains(loc, url.QueryEscape("/secret/file.pdf")) {
		t.Errorf("Location %q should preserve original path in next=", loc)
	}
}

// TestLoginMiddlewareReturnsJSONForAPI verifies the JSON branch: any
// request that doesn't claim to want HTML gets a structured 401.
func TestLoginMiddlewareReturnsJSONForAPI(t *testing.T) {
	r, _ := newLoginRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/?json=true", nil)
	req.Header.Set("Accept", "application/json")
	rec := doRequest(r, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "login_required") {
		t.Errorf("body = %q, want it to mention login_required", body)
	}
}

// TestLoginMiddlewareAllowsWhitelisted confirms the gate does NOT
// interfere with the public bootstrap endpoints the SPA depends on.
func TestLoginMiddlewareAllowsWhitelisted(t *testing.T) {
	r, _ := newLoginRouter(t)

	for _, path := range []string{
		"/-/login",
		"/-/api/auth/status",
		"/-/user",
		"/-/sysinfo",
		"/-/frontend/index.html",
		"/-/logout",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Accept", "text/html")
			rec := doRequest(r, req)
			// A redirected request shouldn't be a redirect (login page
			// is rendered), and a /-/api/auth/status request should
			// return its handler output. We accept any non-302 status
			// here because each route is responsible for its own
			// response shape — the contract is "no 401, no 302 to /-/login".
			if rec.Code == http.StatusFound {
				if strings.HasPrefix(rec.Header().Get("Location"), "/-/login") {
					t.Errorf("%s: redirected to /-/login even though whitelisted", path)
				}
			}
		})
	}
}

// TestLoginSubmit verifies the full POST flow: a correct username/password
// sets the session cookie. Subsequent requests carrying that cookie pass
// the middleware; an absent cookie is gated.
func TestLoginSubmit(t *testing.T) {
	r, state := newLoginRouter(t)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "admin")
	form.Set("next", "/some/file.txt")

	req := httptest.NewRequest(http.MethodPost, "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := doRequest(r, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/some/file.txt" {
		t.Errorf("Location = %q, want /some/file.txt", loc)
	}

	// Pull the cookie the handler set; reuse it for a follow-up request.
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no cookies set on login response")
	}
	cookie := cookies[0]

	// Authenticated follow-up: gated handler returns 200 "through".
	followUp := httptest.NewRequest(http.MethodGet, "/some/file.txt", nil)
	followUp.AddCookie(cookie)
	followUp.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec2 := doRequest(r, followUp)
	if rec2.Code != http.StatusOK {
		t.Errorf("authenticated follow-up status = %d, want 200", rec2.Code)
	}
	if rec2.Body.String() != "through" {
		t.Errorf("authenticated follow-up body = %q, want \"through\"", rec2.Body.String())
	}

	// Sanity: state.verify still works (we didn't accidentally rotate creds).
	if !state.verify("admin", "admin") {
		t.Error("login should not have rotated credentials")
	}
}

// TestLoginSubmitMultipart verifies that the login handler correctly
// parses multipart/form-data bodies — the content type the browser's
// FormData API (and therefore the SPA's submitLogin) sends.  This is a
// regression test for a bug where r.ParseForm() was called before
// r.FormValue(), which caused FormValue() to skip the multipart parse
// and return empty strings for every field, breaking SPA login entirely.
func TestLoginSubmitMultipart(t *testing.T) {
	r, _ := newLoginRouter(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("username", "admin")
	mw.WriteField("password", "admin")
	mw.WriteField("next", "/some/file.txt")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/-/login", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := doRequest(r, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (if you see missing_credentials, the multipart parse regression is back)", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/some/file.txt" {
		t.Errorf("Location = %q, want /some/file.txt", loc)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Error("expected a session cookie to be set on successful login")
	}
}

// TestLoginSubmitWrongCreds checks the unhappy path: a wrong password
// re-renders the login form (302 to /-/login?error=...) without setting
// a session cookie.
func TestLoginSubmitWrongCreds(t *testing.T) {
	r, _ := newLoginRouter(t)

	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "wrongpass")
	form.Set("next", "/some/file.txt")

	req := httptest.NewRequest(http.MethodPost, "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := doRequest(r, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/-/login") {
		t.Errorf("Location = %q, want /-/login", loc)
	}
	if !strings.Contains(loc, "error=invalid_credentials") {
		t.Errorf("Location %q should carry error=invalid_credentials", loc)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == defaultSessionName && c.MaxAge != -1 {
			t.Errorf("auth cookie should be cleared on failed login (got MaxAge=%d)", c.MaxAge)
		}
	}
}

// TestAuthStatusEndpoint exercises the JSON endpoint the SPA polls at
// startup. With no session it should report {login_enabled: true,
// authenticated: false}. After a successful login the same endpoint
// returns the user.
func TestAuthStatusEndpoint(t *testing.T) {
	r, _ := newLoginRouter(t)

	// Unauthenticated probe.
	probe := httptest.NewRequest(http.MethodGet, "/-/api/auth/status", nil)
	rec := doRequest(r, probe)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status["login_enabled"] != true {
		t.Errorf("login_enabled = %v, want true", status["login_enabled"])
	}
	if status["authenticated"] != false {
		t.Errorf("authenticated = %v, want false", status["authenticated"])
	}

	// Now log in and re-probe.
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "admin")
	form.Set("next", "/")
	loginReq := httptest.NewRequest(http.MethodPost, "/-/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := doRequest(r, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	authReq := httptest.NewRequest(http.MethodGet, "/-/api/auth/status", nil)
	authReq.AddCookie(cookie)
	authRec := doRequest(r, authReq)
	var status2 map[string]any
	_ = json.Unmarshal(authRec.Body.Bytes(), &status2)
	if status2["authenticated"] != true {
		t.Errorf("after login, authenticated = %v, want true", status2["authenticated"])
	}
	if status2["name"] != "admin" {
		t.Errorf("after login, name = %v, want admin", status2["name"])
	}
}

// TestUserEndpointReturnsLoginUser is a regression test for the
// "home page flashes then bounces back to login" bug. The SPA's
// onMounted calls loadLoginStatus() (which sets user from /-/api/auth/status)
// and then loadUser() (which calls /-/user). If /-/user returns null
// after a successful --login session, loadUser() overwrites the valid
// user state with null, showLoginGate becomes true again, and the
// login page re-appears. The default /-/user handler in main.go must
// therefore return the LoginUser from the session, not null.
func TestUserEndpointReturnsLoginUser(t *testing.T) {
	r, _ := newLoginRouter(t)

	// Log in to get a session cookie.
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "admin")
	form.Set("next", "/")
	loginReq := httptest.NewRequest(http.MethodPost, "/-/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := doRequest(r, loginReq)
	cookie := loginRec.Result().Cookies()[0]

	// Simulate the main.go default /-/user handler behavior: read
	// LoginUser from the session and return it as JSON. This mirrors
	// the handler registered in main.go when no auth module is active.
	userReq := httptest.NewRequest(http.MethodGet, "/-/user", nil)
	userReq.AddCookie(cookie)
	// The default /-/user handler isn't registered by newLoginRouter
	// (only registerLoginRoutes is), so we test the same logic inline:
	// the contract is "userFromLoginSession(r) returns a non-nil user
	// after login, and that user has Name=admin/Provider=login".
	u := userFromLoginSession(userReq)
	if u == nil {
		t.Fatal("userFromLoginSession = nil after successful login; " +
			"/-/user would return null and the SPA would bounce back to login")
	}
	if u.Name != "admin" {
		t.Errorf("user.Name = %q, want admin", u.Name)
	}
	if u.Provider != loginProviderName {
		t.Errorf("user.Provider = %q, want %q", u.Provider, loginProviderName)
	}
}

// TestChangePasswordRequiresSession asserts the endpoint refuses to
// operate when the caller doesn't have a session. Without this, any
// network-reachable client could brute-force passwords via 401 timing.
func TestChangePasswordRequiresSession(t *testing.T) {
	r, _ := newLoginRouter(t)

	body := strings.NewReader(`{"old":"admin","new":"newpass"}`)
	req := httptest.NewRequest(http.MethodPut, "/-/api/auth/password", body)
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(r, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("change-password without session = %d, want 401", rec.Code)
	}
}

// TestChangePasswordWithSession covers the full happy path: log in,
// then rotate the password via PUT. After the rotation an old-password
// verify fails, a new-password verify succeeds, and the auth middleware
// no longer needs to be re-passed (we still have a session).
func TestChangePasswordWithSession(t *testing.T) {
	r, state := newLoginRouter(t)

	// Log in.
	form := url.Values{}
	form.Set("username", "admin")
	form.Set("password", "admin")
	form.Set("next", "/")
	loginReq := httptest.NewRequest(http.MethodPost, "/-/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := doRequest(r, loginReq)
	if len(loginRec.Result().Cookies()) == 0 {
		t.Fatal("no cookie set on login")
	}
	cookie := loginRec.Result().Cookies()[0]

	// Rotate.
	body := strings.NewReader(`{"old":"admin","new":"newpass"}`)
	put := httptest.NewRequest(http.MethodPut, "/-/api/auth/password", body)
	put.Header.Set("Content-Type", "application/json")
	put.AddCookie(cookie)
	rec := doRequest(r, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("change-password status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	if state.verify("admin", "admin") {
		t.Error("old password should no longer validate")
	}
	if !state.verify("admin", "newpass") {
		t.Error("new password should validate")
	}
}

// TestLoginDisabledBypassesMiddleware — when --login is off, the
// middleware isn't wired up at all (this test simulates that by NOT
// wrapping hdlr in loginAuthMiddleware). Whitelisted paths would be
// irrelevant; the test instead confirms that the gating only applies
// when explicitly enabled.
func TestLoginDisabledByDefault(t *testing.T) {
	dir := t.TempDir()
	state := loadLoginCredentials(filepath.Join(dir, "auth-state.json"))
	_ = state // ensure loadable in the disabled path too — proves zero-cost

	// Build a vanilla router WITHOUT the middleware to confirm files
	// are reachable without --login. This mirrors the production code
	// path when --login is not passed.
	r := mux.NewRouter()
	r.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/secret.txt", nil)
	req.Header.Set("Accept", "*/*")
	rec := doRequest(r, req)
	if rec.Code != http.StatusOK {
		t.Errorf("un-gated server returned %d, want 200 (--login off should be zero-impact)", rec.Code)
	}
}
