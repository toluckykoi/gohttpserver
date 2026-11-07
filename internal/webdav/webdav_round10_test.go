package webdav

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codeskyblue/gohttpserver/internal/auth"
	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

// 第 10 轮性能审查回归：
//  1. VerifyWebdav 的 bcrypt 移到 s.mu 之外——并发未命中不再阻塞管理员账号操作。
//  2. PUT 覆盖旧文件的配额口径不变（preflightPut 的 oldSize 经 context 复用）。

// 缓存命中路径返回的账号必须是当前快照：锁外验证期间账号被修改后，
// ReadOnly/Remark 等属性不得停留在验证时的旧值。
func TestRound10VerifyReturnsFreshAccount(t *testing.T) {
	st := newPerfDavState(t)
	acc, pw, err := st.CreateAccount(CreateAccountRequest{Remark: "before"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DeleteAccount(acc.ID) })

	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Fatal("first verify should succeed")
	}

	// 破坏存储哈希（证明第二次走缓存）+ 修改账号属性。
	st.mu.Lock()
	for i := range st.data.Accounts {
		if st.data.Accounts[i].ID == acc.ID {
			st.data.Accounts[i].PasswordSHA = "$2a$10$corruptedcorruptedcorruptedcorruptedcorruptedcorruptedco"
			st.data.Accounts[i].ReadOnly = true
			st.data.Accounts[i].Remark = "after"
		}
	}
	st.mu.Unlock()

	got, ok := st.VerifyWebdav("admin", pw)
	if !ok {
		t.Fatal("second verify should hit credential cache, but failed")
	}
	if !got.ReadOnly || got.Remark != "after" {
		t.Errorf("verify returned stale account snapshot: readOnly=%v remark=%q", got.ReadOnly, got.Remark)
	}
}

// bcrypt 在锁外执行：并发验证未命中（真跑 bcrypt）期间，写锁必须能立即取得。
// 旧实现整个验证过程持有 RLock，写者会排队到 bcrypt 结束。此测试主要价值在
// -race 下暴露数据竞争；无死锁即通过。
func TestRound10VerifyBcryptOutsideLock(t *testing.T) {
	st := newPerfDavState(t)
	if _, _, err := st.CreateAccount(CreateAccountRequest{Remark: "lock"}, "admin"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// 错误密码走慢路径全价 bcrypt，永不写缓存。
				st.VerifyWebdav("admin", "wrong-password")
			}
		}()
	}
	for i := 0; i < 50; i++ {
		done := make(chan struct{})
		go func() {
			st.mu.Lock()
			st.mu.Unlock()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("write lock blocked >5s while verifies in flight (bcrypt still holding RLock)")
		}
	}
	close(stop)
	wg.Wait()
}

// PUT 覆盖旧文件：配额预检扣除旧大小、Close 记账 delta=new-old，两点合起来
// 依赖 preflightPut 与 OpenFile 对同一目标的一致 oldSize。注入路径错误
// （如 oldSize 丢失按 0 记）会使 usage 偏大一个旧文件体积，此测试即失败。
func TestRound10PutOverwriteQuotaAccounting(t *testing.T) {
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
	hash, err := auth.Generate("round10-pass")
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	st.data.Accounts = append(st.data.Accounts, WebdavAccount{
		ID: "wd_round10", Remark: "round10", Username: "admin",
		PasswordSHA: hash, RootPath: "/", QuotaBytes: 5000,
	})
	st.mu.Unlock()

	usage := LoadUsageState(db)
	srv := NewWebdavServer(tmp, st, usage)

	old := strings.Repeat("A", 1000)
	if err := os.WriteFile(filepath.Join(tmp, "file.txt"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	// 用启动期的全树重算把 usage 种子化为真实磁盘占用（与生产口径一致）。
	if err := usage.Recalculate(tmp, "wd_round10"); err != nil {
		t.Fatal(err)
	}
	if got := usage.Get("wd_round10"); got != 1000 {
		t.Fatalf("seeded usage = %d, want 1000", got)
	}

	req := httptest.NewRequest("PUT", "/dav/file.txt", strings.NewReader(strings.Repeat("B", 2000)))
	req.SetBasicAuth("admin", "round10-pass")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("PUT overwrite -> %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}

	// 记账口径：1000（旧值）+ (2000 - 1000) = 2000。若 OpenFile 复用 oldSize 失败
	// 退回自行 stat，结果相同；若注入了错误的 oldSize（0），会得到 3000。
	if got := usage.Get("wd_round10"); got != 2000 {
		t.Errorf("usage after overwrite = %d, want 2000 (old size must be deducted exactly once)", got)
	}
	got, err := os.ReadFile(filepath.Join(tmp, "file.txt"))
	if err != nil || string(got) != strings.Repeat("B", 2000) {
		t.Errorf("overwritten content wrong: len=%d err=%v", len(got), err)
	}
}
