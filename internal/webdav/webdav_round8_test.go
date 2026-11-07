package webdav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

// 第 8 轮审计回归：
//  1. WebDAV 读通道此前完全不拦服务器私有文件（GET /dav/gohttpserver.db 可下载
//     凭据库）；现以 404 拒绝且与 ProtectSystemFiles 开关无关。
//  2. 写保护名单曾可被 Windows 名字变体（尾点/ADS）绕过。
//  3. generateWebdavID 数组越界 panic 导致 CreateAccount 必然失败。
//  4. 限流映射满时 fail-open。

func newRound8DavServer(t *testing.T, protectSystemFiles bool) (*WebdavServer, string) {
	t.Helper()
	tmp := t.TempDir()
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	st := LoadWebdavAccountsAt(db, ":memory:")
	if err := st.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.Generate("round8-pass")
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.data.Accounts = append(st.data.Accounts, WebdavAccount{
		ID: "wd_round8", Remark: "round8", Username: "admin",
		PasswordSHA: hash, RootPath: "/", ProtectSystemFiles: protectSystemFiles,
	})
	st.mu.Unlock()
	usage := LoadUsageState(db)
	return NewWebdavServer(tmp, st, usage), tmp
}

func davRound8(t *testing.T, srv *WebdavServer, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.SetBasicAuth("admin", "round8-pass")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestRound8DavProtectedReadsBlocked(t *testing.T) {
	for _, protect := range []bool{false, true} {
		srv, root := newRound8DavServer(t, protect)
		os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("DB-SECRET"), 0o644)
		os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte("token: SECRET"), 0o644)
		os.WriteFile(filepath.Join(root, "frp-encryption.key"), []byte("KEY-MATERIAL"), 0o644)

		for _, p := range []string{"/dav/gohttpserver.db", "/dav/.ghs.yml", "/dav/frp-encryption.key"} {
			rec := davRound8(t, srv, "GET", p, "")
			if rec.Code != http.StatusNotFound {
				t.Errorf("protect=%v GET %s -> %d, want 404 (deny without confirming existence)", protect, p, rec.Code)
			}
			if strings.Contains(rec.Body.String(), "SECRET") || strings.Contains(rec.Body.String(), "KEY-MATERIAL") {
				t.Errorf("protect=%v GET %s leaked protected content", protect, p)
			}
		}
	}
}

func TestRound8DavProtectedWritesBlockedEvenWithoutFlag(t *testing.T) {
	srv, root := newRound8DavServer(t, false) // ProtectSystemFiles 关闭
	os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("DB-SECRET"), 0o644)
	os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte("upload: false\n"), 0o644)

	for _, p := range []string{
		"/dav/gohttpserver.db", "/dav/gohttpserver.db.", "/dav/gohttpserver.db::$DATA",
		"/dav/.ghs.yml", "/dav/.ghs.yml.", "/dav/.ghs.yml::$DATA",
	} {
		rec := davRound8(t, srv, "PUT", p, "EVIL")
		if rec.Code == http.StatusCreated {
			t.Errorf("PUT %s -> %d, must be denied", p, rec.Code)
		}
	}
	if got, err := os.ReadFile(filepath.Join(root, "gohttpserver.db")); err != nil || string(got) != "DB-SECRET" {
		t.Errorf("gohttpserver.db tampered via WebDAV: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".ghs.yml")); err != nil || string(got) != "upload: false\n" {
		t.Errorf(".ghs.yml tampered via WebDAV: %q err=%v", got, err)
	}
}

func TestRound8GenerateWebdavIDValid(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id, err := generateWebdavID()
		if err != nil {
			t.Fatalf("generateWebdavID: %v", err)
		}
		if len(id) != 9 || !strings.HasPrefix(id, "wd_") {
			t.Fatalf("generateWebdavID produced %q, want wd_ + 6 hex chars", id)
		}
		for _, c := range id[3:] {
			if !strings.ContainsRune("0123456789abcdef", c) {
				t.Fatalf("generateWebdavID produced non-hex %q", id)
			}
		}
		seen[id] = true
	}
	if len(seen) < 90 {
		t.Errorf("IDs collide too often: %d unique out of 100", len(seen))
	}
}

func TestRound8CreateAccountEndToEnd(t *testing.T) {
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := LoadWebdavAccountsAt(db, ":memory:")
	// 旧实现在 generateWebdavID 处必然 panic；CreateAccount 应正常完成。
	acc, pw, err := st.CreateAccount(CreateAccountRequest{Remark: "e2e"}, "admin")
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if !strings.HasPrefix(acc.ID, "wd_") || len(acc.ID) != 9 {
		t.Errorf("unexpected account id %q", acc.ID)
	}
	if len(pw) != 10 {
		t.Errorf("unexpected password length %d", len(pw))
	}
	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Error("created account cannot verify its password")
	}
}

func TestRound8RateLimiterEvictsInsteadOfFailOpen(t *testing.T) {
	l := newAuthRateLimiter(5, time.Minute)
	newIP := "203.0.113.77"
	for i := 0; i < authRateLimiterMaxEntries; i++ {
		l.fail(fakeRound8IP(i))
	}
	if len(l.attempts) != authRateLimiterMaxEntries {
		t.Fatalf("map size %d after fill, want %d", len(l.attempts), authRateLimiterMaxEntries)
	}
	// 旧实现在映射满时直接丢弃新 IP 的失败记录，限流 fail-open。
	l.fail(newIP)
	l.mu.Lock()
	_, recorded := l.attempts[newIP]
	size := len(l.attempts)
	l.mu.Unlock()
	if !recorded {
		t.Error("new IP failure not recorded when map is full (fail-open regression)")
	}
	if size > authRateLimiterMaxEntries {
		t.Errorf("map size %d exceeds hard cap", size)
	}
}

func fakeRound8IP(i int) string {
	return "198.51.100." + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
}
