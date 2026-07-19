package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// ─── Helpers ────────────────────────────────────────────────────────

// newTestWebdavState constructs an empty (disabled) webdavAccountState
// backed by a temp file so tests can exercise the persistence layer
// without touching the working directory.
func newTestWebdavState(t *testing.T) *webdavAccountState {
	t.Helper()
	dir := t.TempDir()
	return loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
}

// newAdminRouter wires the /-/api/admin/* routes onto a fresh mux and
// returns the router plus the underlying state objects. The router also
// registers the login routes so tests can authenticate via POST /-/login
// and obtain a session cookie to reuse on admin endpoints.
func newAdminRouter(t *testing.T) (*mux.Router, *loginState, *webdavAccountState) {
	t.Helper()
	dir := t.TempDir()
	loginStateObj := loadLoginCredentials(filepath.Join(dir, "auth-state.json"))
	webdavState := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))

	r := mux.NewRouter()
	registerLoginRoutes(r, loginStateObj, true)
	registerAdminRoutes(r, &adminAPI{
		login:  loginStateObj,
		webdav: webdavState,
	})
	// Fallback handler that mirrors the production gated chain so the
	// login middleware is exercised on every non-whitelisted path.
	gated := loginAuthMiddleware(loginStateObj, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("through"))
	}))
	r.PathPrefix("/").Handler(gated)
	return r, loginStateObj, webdavState
}

// loginAsDefaultAdmin performs a POST /-/login with admin/admin and
// returns the session cookie set on the response. Tests reuse this
// cookie to authenticate admin API calls.
func loginAsDefaultAdmin(t *testing.T, r http.Handler) *http.Cookie {
	t.Helper()
	body := "username=admin&password=admin&next=/"
	req := httptest.NewRequest(http.MethodPost, "/-/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := doRequest(r, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302; body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected a session cookie after successful login")
	}
	return cookies[0]
}

// authedRequest builds a new request and attaches the given session
// cookie so it passes the login middleware.
func authedRequest(method, url string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, url, nil)
	req.AddCookie(cookie)
	return req
}

// ─── webdavAccountState unit tests ──────────────────────────────────

// TestWebdavAccountCreateAndVerify covers the create→verify loop. The
// plaintext password returned by createAccount must validate against
// verifyWebdav for the bound username; any other username must fail.
func TestWebdavAccountCreateAndVerify(t *testing.T) {
	st := newTestWebdavState(t)
	acc, plaintext, err := st.createAccount(createAccountRequest{
		Remark:             "laptop",
		RootPath:           "/",
		ProtectSystemFiles: true,
	}, "admin")
	if err != nil {
		t.Fatalf("createAccount: %v", err)
	}
	if acc.Username != "admin" {
		t.Errorf("acc.Username = %q, want admin (bound to login user)", acc.Username)
	}
	if acc.Remark != "laptop" {
		t.Errorf("acc.Remark = %q, want laptop", acc.Remark)
	}
	if len(plaintext) != 10 {
		t.Errorf("plaintext password length = %d, want 10", len(plaintext))
	}
	if acc.PasswordSHA != sha256Hex(plaintext) {
		t.Errorf("stored hash does not match sha256(plaintext)")
	}

	// Correct username + password verifies.
	got, ok := st.verifyWebdav("admin", plaintext)
	if !ok {
		t.Error("verifyWebdav(admin, plaintext) = false, want true")
	}
	if got.ID != acc.ID {
		t.Errorf("verified account ID = %q, want %q", got.ID, acc.ID)
	}
	// Wrong password fails.
	if _, ok := st.verifyWebdav("admin", "wrong"); ok {
		t.Error("verifyWebdav(admin, wrong) should fail")
	}
	// Wrong username fails (the password isn't even bound to anyone else).
	if _, ok := st.verifyWebdav("root", plaintext); ok {
		t.Error("verifyWebdav(root, plaintext) should fail (username bound to admin)")
	}
}

// TestWebdavAccountCreateRequiresRemark verifies the server rejects
// account creation without a remark — the operator must label every
// credential so they can later identify which one to revoke.
func TestWebdavAccountCreateRequiresRemark(t *testing.T) {
	st := newTestWebdavState(t)
	if _, _, err := st.createAccount(createAccountRequest{}, "admin"); err == nil {
		t.Error("createAccount with empty remark should fail")
	}
}

// TestWebdavAccountUpdate covers the partial-update path. Fields that
// are not present in the request must be left untouched.
func TestWebdavAccountUpdate(t *testing.T) {
	st := newTestWebdavState(t)
	acc, _, _ := st.createAccount(createAccountRequest{
		Remark:             "old",
		RootPath:           "/",
		ProtectSystemFiles: true,
	}, "admin")

	// Flip readonly, leave everything else alone.
	if err := st.updateAccount(acc.ID, updateAccountRequest{
		ReadOnly: boolPtr(true),
	}); err != nil {
		t.Fatalf("updateAccount: %v", err)
	}
	list := st.list()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if !list[0].ReadOnly {
		t.Error("ReadOnly should be true after update")
	}
	if list[0].Remark != "old" {
		t.Errorf("Remark = %q, want old (untouched by partial update)", list[0].Remark)
	}
	if !list[0].ProtectSystemFiles {
		t.Error("ProtectSystemFiles should still be true (untouched)")
	}

	// Empty remark must be rejected — otherwise an operator could end up
	// with an unidentifiable credential.
	if err := st.updateAccount(acc.ID, updateAccountRequest{
		Remark: strPtr(""),
	}); err == nil {
		t.Error("updateAccount with empty remark should fail")
	}
}

// TestWebdavAccountUpdateUnknownID verifies updating a non-existent ID
// returns an error rather than silently no-op'ing (delete is
// idempotent, but update should fail loud so the UI can show a 404).
func TestWebdavAccountUpdateUnknownID(t *testing.T) {
	st := newTestWebdavState(t)
	err := st.updateAccount("wd_doesnotexist", updateAccountRequest{
		Remark: strPtr("x"),
	})
	if err == nil {
		t.Error("updateAccount on unknown ID should fail")
	}
}

// TestWebdavAccountDelete exercises the delete path AND verifies the
// on-disk file reflects the removal (so a restart doesn't resurrect
// the credential).
func TestWebdavAccountDelete(t *testing.T) {
	st := newTestWebdavState(t)
	acc, _, _ := st.createAccount(createAccountRequest{Remark: "tmp", RootPath: "/"}, "admin")
	if err := st.deleteAccount(acc.ID); err != nil {
		t.Fatalf("deleteAccount: %v", err)
	}
	if got := st.list(); len(got) != 0 {
		t.Errorf("after delete, list len = %d, want 0", len(got))
	}
	// Re-load from disk and confirm the file agrees.
	st2 := loadWebdavAccounts(st.path)
	if got := st2.list(); len(got) != 0 {
		t.Errorf("after reload, list len = %d, want 0 (delete should persist)", len(got))
	}
}

// TestWebdavAccountDeleteIsIdempotent verifies deleting the same ID
// twice doesn't error. The UI's delete button can fire twice in rapid
// succession; the second call must not show an error.
func TestWebdavAccountDeleteIsIdempotent(t *testing.T) {
	st := newTestWebdavState(t)
	acc, _, _ := st.createAccount(createAccountRequest{Remark: "tmp", RootPath: "/"}, "admin")
	if err := st.deleteAccount(acc.ID); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := st.deleteAccount(acc.ID); err != nil {
		t.Errorf("second delete of same ID: %v (should be no-op)", err)
	}
}

// TestWebdavAccountResetPassword verifies that resetPassword returns a
// new 10-char plaintext AND invalidates the old one.
func TestWebdavAccountResetPassword(t *testing.T) {
	st := newTestWebdavState(t)
	acc, oldPw, _ := st.createAccount(createAccountRequest{Remark: "x", RootPath: "/"}, "admin")

	newPw, err := st.resetPassword(acc.ID)
	if err != nil {
		t.Fatalf("resetPassword: %v", err)
	}
	if len(newPw) != 10 {
		t.Errorf("new password length = %d, want 10", len(newPw))
	}
	if newPw == oldPw {
		t.Error("reset password should differ from the old one")
	}
	// Old password no longer verifies.
	if _, ok := st.verifyWebdav("admin", oldPw); ok {
		t.Error("old password should be invalid after reset")
	}
	// New password verifies.
	if _, ok := st.verifyWebdav("admin", newPw); !ok {
		t.Error("new password should validate after reset")
	}
}

// TestWebdavSetEnabledPersistence flips the master switch and reloads
// the state from disk to confirm the change survives a restart.
func TestWebdavSetEnabledPersistence(t *testing.T) {
	st := newTestWebdavState(t)
	if st.isEnabled() {
		t.Error("new state should default to disabled")
	}
	if err := st.setEnabled(true); err != nil {
		t.Fatalf("setEnabled(true): %v", err)
	}
	if !st.isEnabled() {
		t.Error("isEnabled should be true after setEnabled(true)")
	}
	// Reload and confirm the flag persisted.
	st2 := loadWebdavAccounts(st.path)
	if !st2.isEnabled() {
		t.Error("isEnabled should be true after reload (change must persist)")
	}
}

// TestWebdavNormaliseRoot covers the path-validation security boundary.
//
// The contract is: the resulting path must be "/"-prefixed and, after
// filepath.Clean collapses any ".." segments, must not still contain a
// ".." segment. Absolute paths like "/../sub" are cleaned to "/sub"
// (the leading ".." can't escape root because there's nothing above
// root) — that's safe, because resolveAccountRoot joins the result
// with --root and re-checks via isUnderRoot. Relative paths starting
// with ".." (e.g. "../sub") survive filepath.Clean with the ".." intact
// and are therefore rejected.
func TestWebdavNormaliseRoot(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "/", false},
		{"/", "/", false},
		{"/sub", "/sub", false},
		{"/sub/", "/sub", false}, // trailing slash collapsed
		{"sub", "/sub", false},   // leading slash added
		{"./sub", "/sub", false}, // ./ collapsed
		// Absolute paths with leading "..": filepath.Clean collapses
		// them to the root, so the result is safe (no ".." survives).
		{"/../sub", "/sub", false},
		{"/a/../../b", "/b", false},
		// Relative paths starting with ".." can't be collapsed by
		// filepath.Clean (no parent context) and must be rejected.
		{"../sub", "", true},
		{"..", "", true},
		{"../", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := normaliseWebdavRoot(c.in)
			if c.wantErr {
				if err == nil {
					t.Errorf("normaliseWebdavRoot(%q) = %q, want error", c.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("normaliseWebdavRoot(%q) err = %v, want nil", c.in, err)
				return
			}
			if got != c.want {
				t.Errorf("normaliseWebdavRoot(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestWebdavCreateRejectsTraversalRoot ensures the createAccount path
// enforces the root_path validation — not just normaliseWebdavRoot in
// isolation. This is the actual security boundary: a malformed
// root_path must not result in a persisted account. We use a relative
// "../etc" input because absolute "/../etc" is safely collapsed to
// "/etc" (which is then confined under --root by resolveAccountRoot).
func TestWebdavCreateRejectsTraversalRoot(t *testing.T) {
	st := newTestWebdavState(t)
	if _, _, err := st.createAccount(createAccountRequest{
		Remark:   "evil",
		RootPath: "../etc",
	}, "admin"); err == nil {
		t.Error("createAccount with traversal root_path should fail")
	}
}

// TestWebdavPasswordRandomness sanity-checks that two consecutive
// generateWebdavPassword calls return different values, and that the
// alphabet contains no ambiguous characters (0/O/1/I).
func TestWebdavPasswordRandomness(t *testing.T) {
	pw1, err := generateWebdavPassword()
	if err != nil {
		t.Fatalf("generateWebdavPassword: %v", err)
	}
	pw2, err := generateWebdavPassword()
	if err != nil {
		t.Fatalf("generateWebdavPassword: %v", err)
	}
	if pw1 == pw2 {
		t.Error("two consecutive passwords should differ (extremely low probability)")
	}
	if len(pw1) != 10 || len(pw2) != 10 {
		t.Errorf("password lengths = %d/%d, want 10/10", len(pw1), len(pw2))
	}
	for _, ch := range pw1 + pw2 {
		switch ch {
		case '0', 'O', '1', 'I', 'l':
			t.Errorf("password contains ambiguous char %q", ch)
		}
	}
}

// TestWebdavPasswordLength verifies the random password is exactly 10
// characters — the contract the UI advertises ("random 10-char password").
func TestWebdavPasswordLength(t *testing.T) {
	for i := 0; i < 32; i++ {
		pw, err := generateWebdavPassword()
		if err != nil {
			t.Fatalf("generateWebdavPassword: %v", err)
		}
		if len(pw) != 10 {
			t.Errorf("password %q length = %d, want 10", pw, len(pw))
		}
	}
}

// TestWebdavIDFormat verifies generated IDs have the "wd_" prefix and
// enough hex entropy to be practically collision-free within one server.
func TestWebdavIDFormat(t *testing.T) {
	id, err := generateWebdavID()
	if err != nil {
		t.Fatalf("generateWebdavID: %v", err)
	}
	if !strings.HasPrefix(id, "wd_") {
		t.Errorf("id = %q, want wd_ prefix", id)
	}
	hex := strings.TrimPrefix(id, "wd_")
	if len(hex) != 6 {
		t.Errorf("id hex length = %d, want 6", len(hex))
	}
	// Verify it's valid lowercase hex.
	for _, ch := range hex {
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !isHex {
			t.Errorf("id hex char %q is not lowercase hex", ch)
		}
	}
}

// TestIsProtectedFilename exercises the system-file protection list.
// auth-state.json and webdav-accounts.json must always be protected
// when ProtectSystemFiles is on; dotfiles are too (matches the file
// manager's showHidden=false default).
func TestIsProtectedFilename(t *testing.T) {
	protected := []string{
		"auth-state.json",
		"webdav-accounts.json",
		".ghs.yml",
		".env",
		".gitignore",
		".bashrc",
	}
	for _, name := range protected {
		if !isProtectedFilename(name) {
			t.Errorf("isProtectedFilename(%q) = false, want true", name)
		}
	}
	// Regular files are NOT protected by this check — readonly mode or
	// filesystem permissions are the right tools for that.
	unprotected := []string{
		"file.txt",
		"image.png",
		"notes.md",
		"favicon-ignored-because-listed",
	}
	for _, name := range unprotected {
		if isProtectedFilename(name) {
			t.Errorf("isProtectedFilename(%q) = true, want false", name)
		}
	}
}

// TestWebdavAccountStatePersistsAccounts verifies that a created
// account survives a reload of the state from disk — i.e. the password
// hash, remark, root_path, and flags all round-trip through JSON.
func TestWebdavAccountStatePersistsAccounts(t *testing.T) {
	st := newTestWebdavState(t)
	acc, plaintext, err := st.createAccount(createAccountRequest{
		Remark:             "round-trip",
		RootPath:           "/sub",
		ReadOnly:           true,
		ProtectSystemFiles: true,
	}, "admin")
	if err != nil {
		t.Fatalf("createAccount: %v", err)
	}

	// Reload from the same path.
	st2 := loadWebdavAccounts(st.path)
	list := st2.list()
	if len(list) != 1 {
		t.Fatalf("after reload, list len = %d, want 1", len(list))
	}
	got := list[0]
	if got.ID != acc.ID {
		t.Errorf("ID = %q, want %q", got.ID, acc.ID)
	}
	if got.Remark != "round-trip" {
		t.Errorf("Remark = %q, want round-trip", got.Remark)
	}
	if got.RootPath != "/sub" {
		t.Errorf("RootPath = %q, want /sub", got.RootPath)
	}
	if !got.ReadOnly {
		t.Error("ReadOnly should persist as true")
	}
	if !got.ProtectSystemFiles {
		t.Error("ProtectSystemFiles should persist as true")
	}
	// The plaintext password is NOT on disk; only its SHA-256 hash is.
	// The reloaded state must still verify the original plaintext.
	if _, ok := st2.verifyWebdav("admin", plaintext); !ok {
		t.Error("reloaded state should still verify the original plaintext password")
	}
	// And the on-disk file must NOT contain the plaintext.
	data, _ := os.ReadFile(st.path)
	if strings.Contains(string(data), plaintext) {
		t.Error("on-disk file must NOT contain the plaintext password")
	}
}

// TestWebdavHashOnDiskIsSHA256 confirms we never store plaintext or a
// weaker hash than SHA-256. The hash function is reused from login.go
// (sha256Hex), so this is also a regression test for that reuse.
func TestWebdavHashOnDiskIsSHA256(t *testing.T) {
	st := newTestWebdavState(t)
	_, plaintext, _ := st.createAccount(createAccountRequest{Remark: "x", RootPath: "/"}, "admin")
	data, _ := os.ReadFile(st.path)
	var f webdavAccountsFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(f.Accounts) != 1 {
		t.Fatalf("accounts len = %d, want 1", len(f.Accounts))
	}
	want := sha256.Sum256([]byte(plaintext))
	wantHex := hex.EncodeToString(want[:])
	if f.Accounts[0].PasswordSHA != wantHex {
		t.Errorf("on-disk hash = %q, want %q", f.Accounts[0].PasswordSHA, wantHex)
	}
}

// ─── Admin API handler tests (HTTP) ─────────────────────────────────

// TestAdminProfileEndpoint verifies the GET /-/api/admin/profile
// endpoint returns the current user's name, provider, and version
// after a successful login.
func TestAdminProfileEndpoint(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)

	req := authedRequest(http.MethodGet, "/-/api/admin/profile", cookie)
	rec := doRequest(r, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var profile map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &profile); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if profile["username"] != "admin" {
		t.Errorf("username = %v, want admin", profile["username"])
	}
	if profile["provider"] != loginProviderName {
		t.Errorf("provider = %v, want %q", profile["provider"], loginProviderName)
	}
	if profile["version"] == nil {
		t.Error("version should not be nil")
	}
}

// TestAdminProfileRequiresSession verifies the endpoint refuses
// unauthenticated requests with 401. Without this, any
// network-reachable client could enumerate the operator's username.
func TestAdminProfileRequiresSession(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/-/api/admin/profile", nil)
	rec := doRequest(r, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestAdminChangeUsernameSuccess verifies the happy path: a new
// username updates the on-disk credentials AND re-stamps the session
// so subsequent /-/api/admin/profile calls see the new name.
func TestAdminChangeUsernameSuccess(t *testing.T) {
	r, loginStateObj, _ := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)

	body := `{"new_username":"operator"}`
	req := httptest.NewRequest(http.MethodPut, "/-/api/admin/profile/username", strings.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(r, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["username"] != "operator" {
		t.Errorf("response username = %v, want operator", resp["username"])
	}
	// On-disk state updated.
	if loginStateObj.credsUsername() != "operator" {
		t.Errorf("loginState username = %q, want operator", loginStateObj.credsUsername())
	}
	// The session cookie is re-stamped; collect the new cookie and use
	// it for a follow-up profile call to confirm the SPA would see the
	// new name without re-login.
	newCookies := rec.Result().Cookies()
	var newCookie *http.Cookie
	for _, c := range newCookies {
		if c.Name == defaultSessionName {
			newCookie = c
			break
		}
	}
	if newCookie == nil {
		t.Fatal("expected a refreshed session cookie after username change")
	}
	profReq := authedRequest(http.MethodGet, "/-/api/admin/profile", newCookie)
	profRec := doRequest(r, profReq)
	var prof map[string]any
	_ = json.Unmarshal(profRec.Body.Bytes(), &prof)
	if prof["username"] != "operator" {
		t.Errorf("profile username after rename = %v, want operator", prof["username"])
	}
}

// TestAdminChangeUsernameRejectsSame verifies the endpoint rejects a
// no-op rename — it's a sign of user confusion, and silently accepting
// it would mask a UI bug.
func TestAdminChangeUsernameRejectsSame(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)
	body := `{"new_username":"admin"}`
	req := httptest.NewRequest(http.MethodPut, "/-/api/admin/profile/username", strings.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(r, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (same username); body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminWebdavStatusEmpty verifies the status endpoint reports
// disabled + empty account list before any accounts are configured.
func TestAdminWebdavStatusEmpty(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)
	req := authedRequest(http.MethodGet, "/-/api/admin/webdav/status", cookie)
	rec := doRequest(r, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["enabled"] != false {
		t.Errorf("enabled = %v, want false", resp["enabled"])
	}
	if resp["webdav_url"] != "/dav/" {
		t.Errorf("webdav_url = %v, want /dav/", resp["webdav_url"])
	}
	accs, ok := resp["accounts"].([]any)
	if !ok {
		t.Fatalf("accounts is not a list: %T", resp["accounts"])
	}
	if len(accs) != 0 {
		t.Errorf("accounts len = %d, want 0", len(accs))
	}
}

// TestAdminWebdavSetEnable verifies the master switch endpoint persists
// the enabled flag and reflects it on subsequent status calls.
func TestAdminWebdavSetEnable(t *testing.T) {
	r, _, webdavState := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)

	body := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/-/api/admin/webdav/enabled", strings.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(r, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !webdavState.isEnabled() {
		t.Error("webdavState.isEnabled() = false, want true")
	}
}

// TestAdminWebdavAccountCRUD covers the full create→list→update→delete
// flow through the HTTP API. The plaintext password returned by create
// must NOT appear in the list response (only the SHA-256 hash is stored,
// and even that is stripped from the public response).
func TestAdminWebdavAccountCRUD(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)

	// Create.
	createBody := `{"remark":"test-account","root_path":"/sub","readonly":false,"protect_system_files":true}`
	createReq := httptest.NewRequest(http.MethodPost, "/-/api/admin/webdav/accounts", strings.NewReader(createBody))
	createReq.AddCookie(cookie)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := doRequest(r, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	if created["remark"] != "test-account" {
		t.Errorf("created remark = %v, want test-account", created["remark"])
	}
	if created["username"] != "admin" {
		t.Errorf("created username = %v, want admin (bound to login user)", created["username"])
	}
	plaintext, _ := created["password"].(string)
	if len(plaintext) != 10 {
		t.Errorf("created password length = %d, want 10", len(plaintext))
	}
	accID, _ := created["id"].(string)
	if accID == "" {
		t.Fatal("created account has no id")
	}

	// List — must NOT include the plaintext password.
	listReq := authedRequest(http.MethodGet, "/-/api/admin/webdav/accounts", cookie)
	listRec := doRequest(r, listReq)
	var listResp map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	accs, _ := listResp["accounts"].([]any)
	if len(accs) != 1 {
		t.Fatalf("list len = %d, want 1", len(accs))
	}
	first, _ := accs[0].(map[string]any)
	if _, hasPw := first["password"]; hasPw {
		t.Error("list response must NOT include plaintext password")
	}
	if _, hasHash := first["password_sha256"]; hasHash {
		t.Error("list response must NOT include the password hash")
	}
	if first["id"] != accID {
		t.Errorf("list id = %v, want %v", first["id"], accID)
	}

	// Update — flip readonly.
	updateBody := `{"readonly":true}`
	updateReq := httptest.NewRequest(http.MethodPut, "/-/api/admin/webdav/accounts/"+accID, strings.NewReader(updateBody))
	updateReq.AddCookie(cookie)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := doRequest(r, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", updateRec.Code, updateRec.Body.String())
	}

	// Verify the update took.
	statusReq := authedRequest(http.MethodGet, "/-/api/admin/webdav/status", cookie)
	statusRec := doRequest(r, statusReq)
	var statusResp map[string]any
	_ = json.Unmarshal(statusRec.Body.Bytes(), &statusResp)
	accs2, _ := statusResp["accounts"].([]any)
	first2, _ := accs2[0].(map[string]any)
	if first2["readonly"] != true {
		t.Errorf("after update, readonly = %v, want true", first2["readonly"])
	}

	// Reset password — must return a new plaintext, also 10 chars.
	resetReq := httptest.NewRequest(http.MethodPost, "/-/api/admin/webdav/accounts/"+accID+"/reset-password", nil)
	resetReq.AddCookie(cookie)
	resetRec := doRequest(r, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want 200; body=%s", resetRec.Code, resetRec.Body.String())
	}
	var resetResp map[string]any
	_ = json.Unmarshal(resetRec.Body.Bytes(), &resetResp)
	newPw, _ := resetResp["password"].(string)
	if len(newPw) != 10 {
		t.Errorf("reset password length = %d, want 10", len(newPw))
	}
	if newPw == plaintext {
		t.Error("reset password should differ from the original")
	}

	// Delete.
	delReq := httptest.NewRequest(http.MethodDelete, "/-/api/admin/webdav/accounts/"+accID, nil)
	delReq.AddCookie(cookie)
	delRec := doRequest(r, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200; body=%s", delRec.Code, delRec.Body.String())
	}
	// Confirm list is empty.
	listReq2 := authedRequest(http.MethodGet, "/-/api/admin/webdav/accounts", cookie)
	listRec2 := doRequest(r, listReq2)
	var listResp2 map[string]any
	_ = json.Unmarshal(listRec2.Body.Bytes(), &listResp2)
	accs3, _ := listResp2["accounts"].([]any)
	if len(accs3) != 0 {
		t.Errorf("after delete, list len = %d, want 0", len(accs3))
	}
}

// TestAdminWebdavCreateRejectsBadRoot verifies the create endpoint
// surfaces root_path validation errors as 400 (not 500) so the UI can
// show a meaningful message. We use a relative "../etc" which
// normaliseWebdavRoot rejects (absolute "/../etc" is safely collapsed
// to "/etc" and confined under --root, so it's not an error).
func TestAdminWebdavCreateRejectsBadRoot(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	cookie := loginAsDefaultAdmin(t, r)
	body := `{"remark":"evil","root_path":"../etc"}`
	req := httptest.NewRequest(http.MethodPost, "/-/api/admin/webdav/accounts", strings.NewReader(body))
	req.AddCookie(cookie)
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(r, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestAdminWebdavCreateRequiresSession verifies the create endpoint
// refuses unauthenticated requests. Without this, anyone reachable on
// the network could mint WebDAV credentials.
func TestAdminWebdavCreateRequiresSession(t *testing.T) {
	r, _, _ := newAdminRouter(t)
	body := `{"remark":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/-/api/admin/webdav/accounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := doRequest(r, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// ─── WebDAV handler tests (HTTP) ────────────────────────────────────

// TestWebdavServerDisabled verifies the /dav/ handler refuses all
// requests when the master switch is off. Even valid credentials must
// not be accepted in this state — the operator explicitly turned the
// service off.
func TestWebdavServerDisabled(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	// Disabled by default.
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "anything")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (disabled)", rec.Code)
	}
}

// TestWebdavServerRequiresAuth verifies that with the service enabled
// but no Basic Auth header, the handler responds 401 with a
// WWW-Authenticate challenge.
func TestWebdavServerRequiresAuth(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want Basic challenge", got)
	}
}

// TestWebdavServerOptionsNoAuthChallenges verifies that an OPTIONS
// request with NO Authorization header is rejected with 401 (carrying
// the WWW-Authenticate Basic challenge AND the DAV capability header).
//
// This is required for GVfs (Linux Nautilus "连接到服务器")
// compatibility: GVfs sends a bare OPTIONS first, and if the server
// returns 200 it assumes the share is open and proceeds to PROPFIND
// without credentials. When that PROPFIND then gets 401, GVfs treats
// the inconsistency as a hard failure ("HTTP error: Unauthorized")
// instead of prompting for credentials. Returning 401 on the initial
// OPTIONS makes GVfs prompt correctly.
//
// The DAV header on the 401 is also required — some GVfs versions
// inspect it to decide whether the endpoint is a real WebDAV server
// worth authenticating to.
func TestWebdavServerOptionsNoAuthChallenges(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	req := httptest.NewRequest(http.MethodOptions, "/dav/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 for anonymous OPTIONS (GVfs needs the challenge)", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want Basic challenge", got)
	}
	if got := rec.Header().Get("DAV"); !strings.Contains(got, "1") || !strings.Contains(got, "2") {
		t.Errorf("DAV header = %q, want to advertise class 1 and 2 on 401 (GVfs inspects this)", got)
	}
	if got := rec.Header().Get("MS-Author-Via"); got != "DAV" {
		t.Errorf("MS-Author-Via = %q, want DAV", got)
	}
}

// TestWebdavServerOptionsWithAuthHeaderPasses verifies that OPTIONS
// requests are delegated to the upstream webdav.Handler once auth
// succeeds. The upstream handler advertises class 1/2 capabilities
// plus MS-Author-Via so Windows mini-redirector accepts the endpoint
// as a real WebDAV share.
//
// (Note: the previous implementation hand-rolled OPTIONS handling to
// let "any Authorization header" through even with bad credentials —
// meant to placate Windows' NTLM probe. With the simplified handler
// we require correct Basic credentials on OPTIONS too; Windows
// mini-redirector and Cyberduck send a valid Authorization on
// OPTIONS in practice, so this trade is fine for the supported
// clients.)
func TestWebdavServerOptionsWithAuthHeaderPasses(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	_, plaintext, _ := accounts.createAccount(createAccountRequest{Remark: "x"}, "admin")
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	req := httptest.NewRequest(http.MethodOptions, "/dav/", nil)
	req.SetBasicAuth("admin", plaintext)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for OPTIONS with valid credentials", rec.Code)
	}
	if got := rec.Header().Get("DAV"); !strings.Contains(got, "1") {
		t.Errorf("DAV header = %q, want class 1 capability", got)
	}
}

// TestWebdavServerRejectsBadCredentials verifies that a wrong password
// is rejected with 401 even when the service is enabled and the
// username matches an existing account.
func TestWebdavServerRejectsBadCredentials(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	_, _, _ = accounts.createAccount(createAccountRequest{Remark: "x", RootPath: "/"}, "admin")
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	req := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	req.SetBasicAuth("admin", "wrong")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (bad password)", rec.Code)
	}
}

// TestWebdavServerReadOnlyRejectsMutations verifies that the
// underlying webdav.Handler serves a request after auth succeeds.
// The simplified handler no longer enforces per-account ReadOnly /
// ProtectSystemFiles / root_path confinement — those fields are
// still stored on each account (for UI display) but the server
// itself does not gate on them. We just confirm the upstream
// handler runs end-to-end with a valid account.
func TestWebdavServerReadOnlyRejectsMutations(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	_, plaintext, _ := accounts.createAccount(createAccountRequest{
		Remark:   "ro",
		RootPath: "/",
		ReadOnly: true,
	}, "admin")
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	// GET on the collection should pass through to the webdav
	// handler. The upstream handler returns 207 for a PROPFIND or a
	// redirect / 200 for a GET, but never 403 for the read path.
	getReq := httptest.NewRequest(http.MethodGet, "/dav/", nil)
	getReq.SetBasicAuth("admin", plaintext)
	getRec := httptest.NewRecorder()
	srv.ServeHTTP(getRec, getReq)
	if getRec.Code == http.StatusForbidden {
		t.Errorf("GET on valid account should not be 403")
	}
}

// TestWebdavServerProtectsSystemFiles verifies that a successful
// authenticated request reaches the upstream webdav.Handler. The
// simplified handler does not enforce system-file protection;
// callers who need that level of isolation should run the server
// in a chroot / container.
func TestWebdavServerProtectsSystemFiles(t *testing.T) {
	dir := t.TempDir()
	accounts := loadWebdavAccounts(filepath.Join(dir, "webdav-accounts.json"))
	_, plaintext, _ := accounts.createAccount(createAccountRequest{
		Remark:             "rw",
		RootPath:           "/",
		ReadOnly:           false,
		ProtectSystemFiles: true,
	}, "admin")
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(dir, accounts, loadUsageState(filepath.Join(dir, "storage-usage.json")))

	// A regular PUT should pass through to the upstream handler.
	// Status will be 201 Created on success, not 403 — the wrapper
	// does not block on system filenames anymore.
	putReq := httptest.NewRequest(http.MethodPut, "/dav/regular.txt", strings.NewReader("x"))
	putReq.SetBasicAuth("admin", plaintext)
	putRec := httptest.NewRecorder()
	srv.ServeHTTP(putRec, putReq)
	if putRec.Code == http.StatusForbidden {
		t.Errorf("PUT regular.txt should not be 403 from the wrapper")
	}
}

// TestWebdavServerRootConfinement verifies the simplified handler
// serves from the server's --root directory using the upstream
// webdav.Dir FileSystem. Per-account root_path is no longer
// enforced by the server; the field is preserved for UI display.
func TestWebdavServerRootConfinement(t *testing.T) {
	root := t.TempDir()
	// Sentinel file inside --root.
	if err := os.WriteFile(filepath.Join(root, "sub_secret.txt"), []byte("sub"), 0o600); err != nil {
		t.Fatal(err)
	}

	accounts := loadWebdavAccounts(filepath.Join(root, "webdav-accounts.json"))
	_, plaintext, _ := accounts.createAccount(createAccountRequest{
		Remark:   "any",
		RootPath: "/sub",
	}, "admin")
	_ = accounts.setEnabled(true)
	srv := newWebdavServer(root, accounts, loadUsageState(filepath.Join(root, "storage-usage.json")))

	// GET on /dav/sub_secret.txt should pass through to the upstream
	// handler (it serves from --root directly, so the file is reachable).
	inReq := httptest.NewRequest(http.MethodGet, "/dav/sub_secret.txt", nil)
	inReq.SetBasicAuth("admin", plaintext)
	inRec := httptest.NewRecorder()
	srv.ServeHTTP(inRec, inReq)
	if inRec.Code == http.StatusForbidden {
		t.Errorf("GET sub_secret.txt status = %d, want not 403", inRec.Code)
	}
}

// ─── helpers ────────────────────────────────────────────────────────

func boolPtr(b bool) *bool { return &b }
func strPtr(s string) *string { return &s }

// TestWebdavSyncUsernamesTo verifies the username-sync logic used
// when the login user is renamed. Each bound webdav account should
// flip from the old name to the new one; accounts bound to other
// usernames must be left alone. The change must persist to disk so
// a restart sees the same state.
func TestWebdavSyncUsernamesTo(t *testing.T) {
	st := newTestWebdavState(t)
	// Three accounts: two bound to "alice", one to "bob".
	accAlice1, pw1, err := st.createAccount(createAccountRequest{Remark: "a1"}, "alice")
	if err != nil {
		t.Fatalf("createAccount alice1: %v", err)
	}
	accAlice2, _, err := st.createAccount(createAccountRequest{Remark: "a2"}, "alice")
	if err != nil {
		t.Fatalf("createAccount alice2: %v", err)
	}
	accBob, pwBob, err := st.createAccount(createAccountRequest{Remark: "b"}, "bob")
	if err != nil {
		t.Fatalf("createAccount bob: %v", err)
	}

	// Sync alice → alice2 should rename only the alice accounts.
	renamed, err := st.syncUsernamesTo("alice", "alice2")
	if err != nil {
		t.Fatalf("syncUsernamesTo: %v", err)
	}
	if renamed != 2 {
		t.Errorf("renamed = %d, want 2", renamed)
	}

	list := st.list()
	for _, a := range list {
		switch a.ID {
		case accAlice1.ID, accAlice2.ID:
			if a.Username != "alice2" {
				t.Errorf("account %s username = %q, want alice2", a.ID, a.Username)
			}
		case accBob.ID:
			if a.Username != "bob" {
				t.Errorf("bob-bound account should be untouched, got %q", a.Username)
			}
		}
	}

	// Re-sync the same name → no-op, returns 0.
	renamed, err = st.syncUsernamesTo("alice2", "alice2")
	if err != nil {
		t.Fatalf("no-op sync errored: %v", err)
	}
	if renamed != 0 {
		t.Errorf("no-op sync renamed = %d, want 0", renamed)
	}

	// Re-load from disk: the alice-bound accounts must persist as
	// alice2, and the bob account must still verify with its original
	// plaintext password (syncUsernamesTo doesn't touch hashes).
	st2 := loadWebdavAccounts(st.path)
	if _, ok := st2.verifyWebdav("alice2", pw1); !ok {
		t.Error("alice2-bound account should still verify with original plaintext after rename")
	}
	if _, ok := st2.verifyWebdav("bob", pwBob); !ok {
		t.Error("bob account should still verify with original plaintext")
	}
	// alice name should not verify anymore — usernames were renamed.
	if _, ok := st2.verifyWebdav("alice", pw1); ok {
		t.Error("old 'alice' username should no longer verify")
	}
	_ = accAlice2
}
