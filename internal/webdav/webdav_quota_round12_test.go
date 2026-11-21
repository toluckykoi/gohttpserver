package webdav

// 第 12 轮审计回归：chunked PUT（无 Content-Length）不得绕过配额。
// 修复前：preflightPut 对 ContentLength<0 直接放行，quotaFile.Write 不检查配额，
// 超额仅事后日志。修复后：Write 在 allowedDelta 边界中断并返回 ErrInsufficientCapacity，
// Close 丢弃临时文件，目标文件与用量均不变化。
import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

// noLenReader 包装 bytes.Reader，避免 httptest.NewRequest 自动推断 Content-Length。
type noLenReader struct{ r io.Reader }

func (n noLenReader) Read(p []byte) (int, error) { return n.r.Read(p) }

func newQuotaDavServer(t *testing.T, quota int64) (*WebdavServer, string, string, string) {
	t.Helper()
	tmp := t.TempDir()
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	st := LoadWebdavAccountsAt(db, ":memory:")
	if err := st.SetEnabled(true); err != nil {
		t.Fatalf("enable webdav: %v", err)
	}
	hash, err := auth.Generate("quota-pass")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	st.mu.Lock()
	st.data.Accounts = append(st.data.Accounts, WebdavAccount{
		ID: "wd_quota", Remark: "quota", Username: "admin",
		PasswordSHA: hash, RootPath: "/", QuotaBytes: quota,
	})
	st.mu.Unlock()
	usage := LoadUsageState(db)
	srv := NewWebdavServer(tmp, st, usage)
	return srv, tmp, "admin:quota-pass", "wd_quota"
}

func TestQuotaChunkedPutLimited(t *testing.T) {
	srv, root, _, accountID := newQuotaDavServer(t, 100)

	req := httptest.NewRequest("PUT", "/dav/big.bin", noLenReader{strings.NewReader(strings.Repeat("A", 200))})
	req.ContentLength = -1 // 模拟 chunked：preflightPut 必须跳过，由 Write 层截断
	req.SetBasicAuth("admin", "quota-pass")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code >= 200 && rec.Code < 300 {
		t.Fatalf("chunked PUT over quota must not succeed, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "big.bin")); !os.IsNotExist(err) {
		t.Fatalf("oversized file must not be committed, stat err=%v", err)
	}
	if used := srv.usage.Get(accountID); used != 0 {
		t.Fatalf("usage must stay 0 after rejected PUT, got %d", used)
	}
}

func TestQuotaChunkedPutWithinQuota(t *testing.T) {
	srv, root, _, accountID := newQuotaDavServer(t, 100)

	req := httptest.NewRequest("PUT", "/dav/ok.bin", noLenReader{bytes.NewReader([]byte(strings.Repeat("B", 50)))})
	req.ContentLength = -1
	req.SetBasicAuth("admin", "quota-pass")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("chunked PUT within quota: got %d want 201, body=%s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(filepath.Join(root, "ok.bin"))
	if err != nil || len(data) != 50 {
		t.Fatalf("committed file wrong: len=%d err=%v", len(data), err)
	}
	if used := srv.usage.Get(accountID); used != 50 {
		t.Fatalf("usage must be 50, got %d", used)
	}
}
