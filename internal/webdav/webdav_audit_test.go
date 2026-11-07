package webdav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

// 自审回归：自管的 MOVE/COPY 语义必须与 gdav 的 Dir 层根守卫同样拒绝把
// chroot 根作为源或目标。Destination 是原始 HTTP 头，不经过 gorilla/mux 的
// 路径清洗，"/dav/." 这类等价根形式必须显式拦截。
func newAuditDavServer(t *testing.T) (*WebdavServer, string) {
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
	hash, err := auth.Generate("audit-pass")
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.data.Accounts = append(st.data.Accounts, WebdavAccount{
		ID: "wd_audit", Remark: "audit", Username: "admin", PasswordSHA: hash, RootPath: "/",
	})
	st.mu.Unlock()
	return NewWebdavServer(tmp, st, LoadUsageState(db)), tmp
}

func auditDav(t *testing.T, srv *WebdavServer, method, target, destination string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.SetBasicAuth("admin", "audit-pass")
	if destination != "" {
		req.Header.Set("Destination", destination)
		req.Header.Set("Overwrite", "T")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func assertRootIntact(t *testing.T, root string) {
	t.Helper()
	for _, rel := range []string{"keep1.txt", filepath.Join("sub", "keep2.txt")} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("share root damaged: %s missing (%v)", rel, err)
		}
	}
}

func TestAuditMoveOntoRootBlocked(t *testing.T) {
	srv, root := newAuditDavServer(t)
	os.WriteFile(filepath.Join(root, "keep1.txt"), []byte("one"), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "keep2.txt"), []byte("two"), 0o644)

	for _, tc := range []struct{ name, dst string }{
		{"dot", "http://example.com/dav/."},
		{"slash", "http://example.com/dav/"},
	} {
		rec := auditDav(t, srv, "MOVE", "/dav/keep1.txt", tc.dst)
		if rec.Code == http.StatusCreated || rec.Code == http.StatusNoContent {
			t.Errorf("%s: MOVE onto root accepted (%d)", tc.name, rec.Code)
		}
		assertRootIntact(t, root)
	}
}

func TestAuditMoveFromRootBlocked(t *testing.T) {
	srv, root := newAuditDavServer(t)
	os.WriteFile(filepath.Join(root, "keep1.txt"), []byte("one"), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "keep2.txt"), []byte("two"), 0o644)

	rec := auditDav(t, srv, "MOVE", "/dav/.", "http://example.com/dav/moved-root")
	if rec.Code == http.StatusCreated || rec.Code == http.StatusNoContent {
		t.Error("MOVE from root accepted")
	}
	assertRootIntact(t, root)
}

// COPY 到根的等价形式由 gdav 的 Dir 层根守卫拦截（ErrInvalid → 403），
// 此测试锁定该库层防线不被绕过。
func TestAuditCopyOntoRootBlocked(t *testing.T) {
	srv, root := newAuditDavServer(t)
	os.WriteFile(filepath.Join(root, "keep1.txt"), []byte("one"), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "keep2.txt"), []byte("two"), 0o644)

	rec := auditDav(t, srv, "COPY", "/dav/keep1.txt", "http://example.com/dav/.")
	if rec.Code == http.StatusCreated || rec.Code == http.StatusNoContent {
		t.Errorf("COPY onto root accepted (%d)", rec.Code)
	}
	assertRootIntact(t, root)
}
