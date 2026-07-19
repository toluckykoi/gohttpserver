package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/net/webdav"
)

// ─── usageState tests ───────────────────────────────────────────────────────

func TestUsageStateLoadOrCreate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage-usage.json")

	st := loadUsageState(path)
	if st == nil {
		t.Fatal("nil state")
	}
	if got := st.get("wd_nonexistent"); got != 0 {
		t.Errorf("get(missing) = %d, want 0", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("loadUsageState wrote %s; expected no file until addDelta", path)
	}
}

func TestUsageStateAddDeltaPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage-usage.json")
	st := loadUsageState(path)

	acct := "wd_test1"
	if err := st.addDelta(acct, 1000); err != nil {
		t.Fatalf("addDelta(1000): %v", err)
	}
	if err := st.addDelta(acct, 500); err != nil {
		t.Fatalf("addDelta(500): %v", err)
	}
	if err := st.addDelta(acct, -300); err != nil {
		t.Fatalf("addDelta(-300): %v", err)
	}
	if got, want := st.get(acct), int64(1200); got != want {
		t.Errorf("get = %d, want %d", got, want)
	}

	st2 := loadUsageState(path)
	if got, want := st2.get(acct), int64(1200); got != want {
		t.Errorf("after reload get = %d, want %d", got, want)
	}
}

func TestUsageStateAddDeltaClampsAtZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "storage-usage.json")
	st := loadUsageState(path)

	acct := "wd_test"
	if err := st.addDelta(acct, -100); err != nil {
		t.Fatalf("addDelta(-100): %v", err)
	}
	if got := st.get(acct); got != 0 {
		t.Errorf("get after negative delta = %d, want 0", got)
	}
}

func TestUsageStateConcurrentAddDelta(t *testing.T) {
	// 50 goroutines each adding +10 must result in exactly +500 — no
	// lost updates. Existing tests don't cover this.
	dir := t.TempDir()
	path := filepath.Join(dir, "storage-usage.json")
	st := loadUsageState(path)

	acct := "wd_concurrent"
	const goroutines = 50
	const delta = int64(10)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = st.addDelta(acct, delta)
		}()
	}
	wg.Wait()

	if got, want := st.get(acct), int64(goroutines*delta); got != want {
		t.Errorf("concurrent get = %d, want %d (lost updates!)", got, want)
	}
}

func TestUsageStateRecalculate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.bin"), bytes.Repeat([]byte("a"), 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.bin"), bytes.Repeat([]byte("b"), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.bin"), bytes.Repeat([]byte("c"), 300), 0o644); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(t.TempDir(), "storage-usage.json")
	st := loadUsageState(statePath)
	if err := st.recalculate(dir, "wd_recalc"); err != nil {
		t.Fatalf("recalculate: %v", err)
	}
	if got, want := st.get("wd_recalc"), int64(600); got != want {
		t.Errorf("recalculated usage = %d, want %d", got, want)
	}
}

func TestWalkSizeSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.bin")
	if err := os.WriteFile(target, bytes.Repeat([]byte("x"), 500), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.bin")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	got, err := walkSize(dir)
	if err != nil {
		t.Fatalf("walkSize: %v", err)
	}
	if want := int64(500); got != want {
		t.Errorf("walkSize = %d, want %d (must skip symlinks)", got, want)
	}
}

// ─── preflightQuota tests ───────────────────────────────────────────────────

func TestPreflightQuotaWithinLimit(t *testing.T) {
	if err := preflightQuota(100, 1000, 0, 200); err != nil {
		t.Errorf("within-limit preflight returned %v, want nil", err)
	}
}

func TestPreflightQuotaExceeds(t *testing.T) {
	err := preflightQuota(800, 1000, 0, 300)
	if err == nil {
		t.Fatal("expected error for over-quota, got nil")
	}
	if !errors.Is(err, ErrInsufficientCapacity) {
		t.Errorf("error = %v, want ErrInsufficientCapacity", err)
	}
}

func TestPreflightQuotaShrinkAllowed(t *testing.T) {
	// Overwriting a 2000-byte file with 100 bytes when used=1500,
	// quota=1000: net delta is -1900, so must be allowed.
	if err := preflightQuota(1500, 1000, 2000, 100); err != nil {
		t.Errorf("shrink preflight returned %v, want nil", err)
	}
}

func TestPreflightQuotaUnlimited(t *testing.T) {
	if err := preflightQuota(1<<30, 0, 0, 1<<30); err != nil {
		t.Errorf("unlimited preflight returned %v, want nil", err)
	}
}

func TestPreflightQuotaChunkedBypasses(t *testing.T) {
	// contentLength < 0 means chunked transfer; we don't have an
	// accurate size, so the preflight must fail open.
	if err := preflightQuota(800, 1000, 0, -1); err != nil {
		t.Errorf("chunked preflight returned %v, want nil (fail-open)", err)
	}
}

// ─── quotaFileSystem tests ──────────────────────────────────────────────────

func newTestQuotaFS(t *testing.T, quotaBytes int64) (*quotaFileSystem, *usageState, string) {
	t.Helper()
	root := t.TempDir()
	usagePath := filepath.Join(t.TempDir(), "storage-usage.json")
	us := loadUsageState(usagePath)
	qfs := &quotaFileSystem{
		inner:      webdav.Dir(root),
		state:      us,
		accountID:  "wd_qfs",
		quotaBytes: quotaBytes,
		root:       root,
		readOnly:   false,
	}
	return qfs, us, root
}

func putFile(t *testing.T, qfs *quotaFileSystem, name string, data []byte) error {
	t.Helper()
	f, err := qfs.OpenFile(t.Context(), name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if len(data) > 0 {
		if _, err := f.Write(data); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func TestQuotaFileSystemPutWithinQuotaTracksUsage(t *testing.T) {
	qfs, us, root := newTestQuotaFS(t, 1000)

	if err := putFile(t, qfs, "/hello.txt", bytes.Repeat([]byte("a"), 500)); err != nil {
		t.Fatalf("putFile: %v", err)
	}
	if got, want := us.get("wd_qfs"), int64(500); got != want {
		t.Errorf("used = %d, want %d", got, want)
	}
	if info, err := os.Stat(filepath.Join(root, "hello.txt")); err != nil || info.Size() != 500 {
		t.Errorf("file on disk: info=%v err=%v", info, err)
	}
}

func TestQuotaFileSystemPutExceedsQuotaAcceptedByPostCheck(t *testing.T) {
	// The post-check accepts over-quota writes (Cloudreve behaviour);
	// the hard rejection happens via preflightQuota in ServeHTTP, not
	// here. This test verifies the documented contract.
	qfs, us, _ := newTestQuotaFS(t, 100)

	if err := putFile(t, qfs, "/big.bin", bytes.Repeat([]byte("b"), 500)); err != nil {
		t.Errorf("post-check putFile returned %v, want nil (post-check accepts)", err)
	}
	if got, want := us.get("wd_qfs"), int64(500); got != want {
		t.Errorf("used = %d, want %d", got, want)
	}
}

func TestQuotaFileSystemOverwriteShrinksUsage(t *testing.T) {
	qfs, us, _ := newTestQuotaFS(t, 10000)

	if err := putFile(t, qfs, "/f.bin", bytes.Repeat([]byte("x"), 1000)); err != nil {
		t.Fatal(err)
	}
	if err := putFile(t, qfs, "/f.bin", bytes.Repeat([]byte("y"), 100)); err != nil {
		t.Fatal(err)
	}
	if got, want := us.get("wd_qfs"), int64(100); got != want {
		t.Errorf("used after shrink = %d, want %d", got, want)
	}
}

func TestQuotaFileSystemDeleteReducesUsage(t *testing.T) {
	qfs, us, root := newTestQuotaFS(t, 10000)

	if err := putFile(t, qfs, "/doomed.bin", bytes.Repeat([]byte("z"), 800)); err != nil {
		t.Fatal(err)
	}
	if got := us.get("wd_qfs"); got != 800 {
		t.Fatalf("pre-delete used = %d, want 800", got)
	}
	if err := qfs.RemoveAll(t.Context(), "/doomed.bin"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if got, want := us.get("wd_qfs"), int64(0); got != want {
		t.Errorf("post-delete used = %d, want %d", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "doomed.bin")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file still on disk after RemoveAll: err=%v", err)
	}
}

func TestQuotaFileSystemReadOnlyRejectsWrites(t *testing.T) {
	root := t.TempDir()
	usagePath := filepath.Join(t.TempDir(), "storage-usage.json")
	us := loadUsageState(usagePath)
	qfs := &quotaFileSystem{
		inner:      webdav.Dir(root),
		state:      us,
		accountID:  "wd_ro",
		quotaBytes: 1000,
		root:       root,
		readOnly:   true,
	}
	if err := putFile(t, qfs, "/x.bin", []byte("hi")); err == nil {
		t.Error("write to read-only FS succeeded, want error")
	}
}

func TestQuotaFileSystemUnlimitedAlwaysAllowed(t *testing.T) {
	qfs, us, _ := newTestQuotaFS(t, 0)
	data := bytes.Repeat([]byte("u"), 1024)
	if err := putFile(t, qfs, "/big.bin", data); err != nil {
		t.Fatalf("unlimited putFile: %v", err)
	}
	if got, want := us.get("wd_qfs"), int64(len(data)); got != want {
		t.Errorf("used = %d, want %d", got, want)
	}
}

// ─── Top-level PROPFIND handler tests ───────────────────────────────────────

func buildTestWebdavServer(t *testing.T, quotaBytes int64) (*webdavServer, string, string) {
	t.Helper()
	root := t.TempDir()
	accountsPath := filepath.Join(t.TempDir(), "webdav-accounts.json")
	usagePath := filepath.Join(t.TempDir(), "storage-usage.json")

	webdavState := loadWebdavAccounts(accountsPath)
	usageStateObj := loadUsageState(usagePath)

	acc, plaintext, err := webdavState.createAccount(createAccountRequest{
		Remark:     "test",
		RootPath:   "/",
		QuotaBytes: quotaBytes,
	}, "admin")
	if err != nil {
		t.Fatalf("createAccount: %v", err)
	}
	// The webdav service is disabled by default — every test in this
	// file expects it enabled, so flip the master switch once.
	if err := webdavState.setEnabled(true); err != nil {
		t.Fatalf("setEnabled: %v", err)
	}
	_ = acc // (returned via plaintext below; ID recoverable via webdavState.list())

	srv := newWebdavServer(root, webdavState, usageStateObj)
	return srv, plaintext, root
}

func TestPropfindRootIncludesQuotaProperties(t *testing.T) {
	srv, pass, root := buildTestWebdavServer(t, 10000)

	// Account IDs are random; look it up so we addDelta against the
	// real ID rather than a hardcoded guess.
	accounts := srv.accounts.list()
	if len(accounts) != 1 {
		t.Fatalf("account count = %d, want 1", len(accounts))
	}
	acctID := accounts[0].ID

	if err := os.WriteFile(filepath.Join(root, "seed.bin"), bytes.Repeat([]byte("q"), 1234), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := srv.usage.addDelta(acctID, 1234); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Depth", "0")
	req.Header.Set("Authorization", basicAuthHeader("admin", pass))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/xml") {
		t.Errorf("Content-Type = %q, want application/xml...", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<D:quota-used-bytes>") {
		t.Errorf("response missing <D:quota-used-bytes>: %s", body)
	}
	if !strings.Contains(body, "<D:quota-available-bytes>") {
		t.Errorf("response missing <D:quota-available-bytes>: %s", body)
	}
	if !strings.Contains(body, "1234") {
		t.Errorf("response missing used value 1234: %s", body)
	}
	// 10000 - 1234 = 8766
	if !strings.Contains(body, "8766") {
		t.Errorf("response missing available value 8766: %s", body)
	}
}

func TestPropfindRootDepthOneIncludesChildren(t *testing.T) {
	srv, pass, root := buildTestWebdavServer(t, 10000)

	if err := os.WriteFile(filepath.Join(root, "child1.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "child2.txt"), []byte("bb"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Depth", "1")
	req.Header.Set("Authorization", basicAuthHeader("admin", pass))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "child1.txt") {
		t.Errorf("response missing child1.txt: %s", body)
	}
	if !strings.Contains(body, "child2.txt") {
		t.Errorf("response missing child2.txt: %s", body)
	}
	// Quota properties must appear exactly once — on the root, not on children.
	if rootQuota := strings.Count(body, "<D:quota-used-bytes>"); rootQuota != 1 {
		t.Errorf("expected exactly 1 <D:quota-used-bytes> (root only), got %d", rootQuota)
	}
}

func TestPropfindSubdirectoryForwardsUpstream(t *testing.T) {
	srv, pass, root := buildTestWebdavServer(t, 0)

	if err := os.WriteFile(filepath.Join(root, "leaf.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("PROPFIND", "/dav/leaf.txt", nil)
	req.Header.Set("Depth", "0")
	req.Header.Set("Authorization", basicAuthHeader("admin", pass))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "<quota-used-bytes>") {
		t.Errorf("subdirectory PROPFIND must not include quota: %s", rec.Body.String())
	}
}

func TestPropfindRootOmitsQuotaWhenUnlimited(t *testing.T) {
	srv, pass, _ := buildTestWebdavServer(t, 0)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Depth", "0")
	req.Header.Set("Authorization", basicAuthHeader("admin", pass))
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<quota-used-bytes>") {
		t.Errorf("unlimited account should NOT emit quota-used-bytes: %s", rec.Body.String())
	}
}

func TestWebdavRequiresAuth(t *testing.T) {
	srv, _, _ := buildTestWebdavServer(t, 1000)

	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(got, "Basic") {
		t.Errorf("WWW-Authenticate = %q, want Basic...", got)
	}
}

// ─── Multistatus XML structure test ─────────────────────────────────────────

// fakeDirEntry is a minimal os.FileInfo for testing the multistatus
// builder without touching the disk.
type fakeDirEntry struct {
	name    string
	size    int64
	isDir   bool
	modTime time.Time
}

func (f fakeDirEntry) Name() string       { return f.name }
func (f fakeDirEntry) Size() int64        { return f.size }
func (f fakeDirEntry) Mode() os.FileMode  { return 0o755 }
func (f fakeDirEntry) ModTime() time.Time { return f.modTime }
func (f fakeDirEntry) IsDir() bool        { return f.isDir }
func (f fakeDirEntry) Sys() interface{}   { return nil }

func TestBuildMultistatusResponseShape(t *testing.T) {
	rootInfo := fakeDirEntry{name: "root", size: 0, isDir: true}
	children := []childInfo{
		{name: "a.txt", size: 100, modTime: time.Unix(1700000000, 0), isDir: false},
		{name: "sub", size: 0, modTime: time.Unix(1700000000, 0), isDir: true},
	}

	ms := buildMultistatusResponse("/dav/", rootInfo, 1234, 10000, children)
	data, err := xml.MarshalIndent(ms, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(data)

	if !strings.Contains(body, "<D:href>/dav/</D:href>") {
		t.Errorf("missing root href: %s", body)
	}
	if !strings.Contains(body, "1234") || !strings.Contains(body, "8766") {
		t.Errorf("missing quota values 1234/8766: %s", body)
	}
	if strings.Count(body, "<D:href>/dav/a.txt</D:href>") != 1 {
		t.Errorf("a.txt entry missing or duplicated")
	}
	if strings.Count(body, "<D:href>/dav/sub</D:href>") != 1 {
		t.Errorf("sub entry missing or duplicated")
	}
	// Children must NOT carry quota props.
	idx := strings.Index(body, "/dav/a.txt")
	if idx < 0 {
		t.Fatal("a.txt href not found")
	}
	end := strings.Index(body[idx:], "<D:response")
	if end < 0 {
		end = strings.Index(body[idx:], "</D:multistatus>")
	}
	block := body[idx : idx+end]
	if strings.Contains(block, "quota-") {
		t.Errorf("a.txt block contains quota props (must be root-only): %s", block)
	}
}

// ─── Admin API quota endpoint tests ─────────────────────────────────────────

func newMinimalAdminHarness(t *testing.T) (*mux.Router, *webdavAccountState, *usageState, string) {
	t.Helper()
	root := t.TempDir()
	loginPath := filepath.Join(t.TempDir(), "auth-state.json")
	webdavPath := filepath.Join(t.TempDir(), "webdav-accounts.json")
	usagePath := filepath.Join(t.TempDir(), "storage-usage.json")

	ls := loadLoginCredentials(loginPath)
	ws := loadWebdavAccounts(webdavPath)
	us := loadUsageState(usagePath)

	r := mux.NewRouter()
	registerLoginRoutes(r, ls, true)
	registerAdminRoutes(r, &adminAPI{login: ls, webdav: ws, usage: us, root: root})
	r.PathPrefix("/").Handler(http.NotFoundHandler())
	return r, ws, us, root
}

// minimalAuthedRequest POSTs to /-/login with default admin/admin
// credentials, captures the session cookie, and returns a request for
// the given endpoint with that cookie attached. Mirrors the
// loginAsDefaultAdmin pattern from admin_api_test.go.
func minimalAuthedRequest(t *testing.T, router *mux.Router, method, url, body string) *http.Request {
	t.Helper()
	loginReq := httptest.NewRequest("POST", "/-/login", strings.NewReader("username=admin&password=admin"))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRec := httptest.NewRecorder()
	router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code >= 400 {
		t.Fatalf("login failed: %d %s", loginRec.Code, loginRec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		cookie = c
		break
	}
	if cookie == nil {
		t.Fatal("no session cookie returned from /-/login")
	}
	var reqBody = strings.NewReader(body)
	req := httptest.NewRequest(method, url, reqBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(cookie)
	return req
}

func TestAdminWebdavCreateWithQuotaPersists(t *testing.T) {
	r, ws, _, _ := newMinimalAdminHarness(t)

	body := `{"remark":"quota-test","root_path":"/","readonly":false,"protect_system_files":true,"quota_bytes":1048576}`
	req := minimalAuthedRequest(t, r, "POST", "/-/api/admin/webdav/accounts", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, _ := resp["quota_bytes"].(float64); int64(got) != 1048576 {
		t.Errorf("quota_bytes = %v, want 1048576", resp["quota_bytes"])
	}
	accounts := ws.list()
	if len(accounts) != 1 {
		t.Fatalf("len(accounts) = %d, want 1", len(accounts))
	}
	if accounts[0].QuotaBytes != 1048576 {
		t.Errorf("persisted QuotaBytes = %d, want 1048576", accounts[0].QuotaBytes)
	}
}

func TestAdminWebdavStatusIncludesUsageFields(t *testing.T) {
	r, ws, us, _ := newMinimalAdminHarness(t)

	acc, _, err := ws.createAccount(createAccountRequest{
		Remark: "x", RootPath: "/", QuotaBytes: 5000,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// Account IDs are "wd_" + random hex; we can't hardcode.
	if err := us.addDelta(acc.ID, 1234); err != nil {
		t.Fatal(err)
	}

	req := minimalAuthedRequest(t, r, "GET", "/-/api/admin/webdav/status", "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	accounts, ok := resp["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("accounts shape: %v", resp["accounts"])
	}
	first := accounts[0].(map[string]any)
	if got, _ := first["quota_bytes"].(float64); int64(got) != 5000 {
		t.Errorf("quota_bytes = %v, want 5000", first["quota_bytes"])
	}
	if got, _ := first["used_bytes"].(float64); int64(got) != 1234 {
		t.Errorf("used_bytes = %v, want 1234", first["used_bytes"])
	}
}

func TestAdminWebdavUpdateQuotaPartial(t *testing.T) {
	r, ws, _, _ := newMinimalAdminHarness(t)
	acc, _, err := ws.createAccount(createAccountRequest{
		Remark: "x", RootPath: "/", QuotaBytes: 100,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	body := `{"quota_bytes":99999}`
	req := minimalAuthedRequest(t, r, "PUT", "/-/api/admin/webdav/accounts/"+acc.ID, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	accounts := ws.list()
	if accounts[0].QuotaBytes != 99999 {
		t.Errorf("QuotaBytes = %d, want 99999", accounts[0].QuotaBytes)
	}
	if accounts[0].Remark != "x" || accounts[0].RootPath != "/" {
		t.Errorf("other fields changed: %+v", accounts[0])
	}
}

func TestAdminWebdavUpdateQuotaRejectsNegative(t *testing.T) {
	r, ws, _, _ := newMinimalAdminHarness(t)
	acc, _, err := ws.createAccount(createAccountRequest{
		Remark: "x", RootPath: "/",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}

	body := `{"quota_bytes":-1}`
	req := minimalAuthedRequest(t, r, "PUT", "/-/api/admin/webdav/accounts/"+acc.ID, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminWebdavRecalculateUsageEndpoint(t *testing.T) {
	r, ws, us, root := newMinimalAdminHarness(t)

	acc, _, err := ws.createAccount(createAccountRequest{
		Remark: "x", RootPath: "/",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// The account's chroot is "/" so files live directly under the
	// harness's root.
	if err := os.WriteFile(filepath.Join(root, "a"), bytes.Repeat([]byte("a"), 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b"), bytes.Repeat([]byte("b"), 300), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "c"), bytes.Repeat([]byte("c"), 600), 0o644); err != nil {
		t.Fatal(err)
	}

	req := minimalAuthedRequest(t, r, "POST", "/-/api/admin/webdav/recalculate-usage", "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Results []struct {
			ID        string `json:"id"`
			OK        bool   `json:"ok"`
			UsedBytes int64  `json:"used_bytes"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !resp.Success || len(resp.Results) != 1 {
		t.Fatalf("response: %+v", resp)
	}
	if resp.Results[0].ID != acc.ID || !resp.Results[0].OK {
		t.Errorf("result entry: %+v", resp.Results[0])
	}
	if resp.Results[0].UsedBytes != 1000 {
		t.Errorf("used_bytes = %d, want 1000", resp.Results[0].UsedBytes)
	}
	if got := us.get(acc.ID); got != 1000 {
		t.Errorf("usage cache = %d, want 1000", got)
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}