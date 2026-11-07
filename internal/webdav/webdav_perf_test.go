package webdav

import (
	"testing"
	"time"

	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

// 第 8 轮性能审查回归：凭据缓存与配额记账单行写。

func newPerfDavState(t *testing.T) *WebdavAccountState {
	t.Helper()
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := LoadWebdavAccountsAt(db, ":memory:")
	if err := st.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	return st
}

// 缓存命中的判定方式：把存储的密码哈希破坏掉。若第二次验证仍成功，
// 必然走的是缓存（真跑 bcrypt 会失败）。
func TestRound9CredCacheAvoidsBcrypt(t *testing.T) {
	st := newPerfDavState(t)
	acc, pw, err := st.CreateAccount(CreateAccountRequest{Remark: "cache"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.DeleteAccount(acc.ID) })

	if _, ok := st.lookupCredCache("admin", pw); ok {
		t.Fatal("cache should be empty before any successful verify")
	}
	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Fatal("first verify should succeed")
	}

	// 破坏存储哈希：缓存命中路径不读哈希，仍应成功。
	st.mu.Lock()
	for i := range st.data.Accounts {
		if st.data.Accounts[i].ID == acc.ID {
			st.data.Accounts[i].PasswordSHA = "$2a$10$corruptedcorruptedcorruptedcorruptedcorruptedcorruptedco"
		}
	}
	st.mu.Unlock()
	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Error("second verify should hit credential cache (bcrypt bypassed), but failed")
	}
}

func TestRound9CredCacheInvalidatedOnChange(t *testing.T) {
	st := newPerfDavState(t)
	acc, pw, err := st.CreateAccount(CreateAccountRequest{Remark: "inv"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Fatal("first verify should succeed")
	}

	newPw, err := st.ResetPassword(acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 旧密码曾因缓存继续有效到 TTL 过期；saveLocked 统一失效后必须立即拒绝。
	if _, ok := st.VerifyWebdav("admin", pw); ok {
		t.Error("old password still verifies after reset (cache not invalidated)")
	}
	if _, ok := st.VerifyWebdav("admin", newPw); !ok {
		t.Error("new password should verify after reset")
	}

	if err := st.DeleteAccount(acc.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.VerifyWebdav("admin", newPw); ok {
		t.Error("deleted account still verifies (cache not invalidated)")
	}
}

func TestRound9CredCacheTTLExpiry(t *testing.T) {
	st := newPerfDavState(t)
	_, pw, err := st.CreateAccount(CreateAccountRequest{Remark: "ttl"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Fatal("first verify should succeed")
	}
	st.credMu.Lock()
	for k, e := range st.credCache {
		e.expiresAt = time.Now().Add(-time.Minute)
		st.credCache[k] = e
	}
	st.credMu.Unlock()
	if _, ok := st.lookupCredCache("admin", pw); ok {
		t.Error("expired cache entry must not hit")
	}
	// 过期后慢路径重验仍成功（密码本身没变）。
	if _, ok := st.VerifyWebdav("admin", pw); !ok {
		t.Error("re-verify after TTL expiry should succeed via slow path")
	}
}

func TestRound9UsageAccountingSingleRowPersist(t *testing.T) {
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	usage := LoadUsageState(db)
	for _, id := range []string{"acc-a", "acc-b", "acc-c"} {
		if err := usage.Recalculate("/nonexistent-root-"+id, id); err != nil {
			t.Fatal(err)
		}
	}

	if err := usage.addDelta("acc-b", 12345); err != nil {
		t.Fatal(err)
	}
	// 重开同一 DB：只有 acc-b 持久化了非零值。
	usage2 := LoadUsageState(db)
	if got := usage2.Get("acc-b"); got != 12345 {
		t.Errorf("acc-b usage = %d, want 12345", got)
	}
	for _, id := range []string{"acc-a", "acc-c"} {
		if got := usage2.Get(id); got != 0 {
			t.Errorf("%s usage = %d, want 0 (no row)", id, got)
		}
	}

	// 归零后该行应被删除（与旧 saveLocked 的清理语义一致）。
	if err := usage.addDelta("acc-b", -12345); err != nil {
		t.Fatal(err)
	}
	usage3 := LoadUsageState(db)
	if got := usage3.Get("acc-b"); got != 0 {
		t.Errorf("acc-b usage after zeroing = %d, want 0 (row deleted)", got)
	}
}
