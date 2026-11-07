package auth

import (
	"testing"

	storedb "github.com/codeskyblue/gohttpserver/internal/store"
)

// 第 10 轮性能审查回归：默认密码判定缓存。
// handleAuthStatus 每次认证探测都调用 isDefaultPassword，旧实现每请求全价
// bcrypt（~60ms）；结果现按凭据缓存，改密/升级/改名即失效。

func newRound10LoginState(t *testing.T) *LoginState {
	t.Helper()
	db, err := storedb.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return LoadLoginCredentialsAt(db, ":memory:")
}

// 缓存命中的判定方式：把存储哈希破坏掉。若第二次判定仍返回 true，
// 必然走的是缓存（真跑 bcrypt 会返回 false）。
func TestRound10DefaultPasswordVerdictCached(t *testing.T) {
	st := newRound10LoginState(t)

	if st.defaultPwdKnown.Load() != defaultPwdUnknown {
		t.Fatalf("verdict should start unknown, got %d", st.defaultPwdKnown.Load())
	}
	if !st.isDefaultPassword() {
		t.Fatal("fresh default credentials should be flagged as default password")
	}
	if st.defaultPwdKnown.Load() != defaultPwdTrue {
		t.Fatalf("verdict should be cached as true, got %d", st.defaultPwdKnown.Load())
	}

	// 破坏存储哈希：缓存命中路径不读哈希，仍应返回 true。
	st.mu.Lock()
	st.creds.PasswordSHA = "$2a$10$corruptedcorruptedcorruptedcorruptedcorruptedcorruptedco"
	st.mu.Unlock()
	if !st.isDefaultPassword() {
		t.Error("second call should hit verdict cache (bcrypt bypassed), but returned false")
	}
}

func TestRound10DefaultPasswordVerdictInvalidatedOnChange(t *testing.T) {
	st := newRound10LoginState(t)

	if !st.isDefaultPassword() {
		t.Fatal("default credentials should be flagged as default password")
	}
	if err := st.changePassword("admin", "new-password-123"); err != nil {
		t.Fatal(err)
	}
	// 改密后缓存必须失效：此时再破坏哈希，判定会走真 bcrypt。
	if st.defaultPwdKnown.Load() != defaultPwdUnknown {
		t.Fatalf("verdict should be unknown after password change, got %d", st.defaultPwdKnown.Load())
	}
	if st.isDefaultPassword() {
		t.Error("changed password still flagged as default (cache not invalidated)")
	}

	// 恢复默认凭据并手动失效：判定应回到 true。
	st.mu.Lock()
	st.creds = defaultLoginCredentials()
	st.mu.Unlock()
	st.invalidateDefaultPasswordCache()
	if !st.isDefaultPassword() {
		t.Error("restored default credentials not flagged as default password")
	}
}

// 用户名参与判定：非 admin 用户名时直接 false，不烧 bcrypt。
func TestRound10DefaultPasswordUsernameMismatch(t *testing.T) {
	st := newRound10LoginState(t)

	if err := st.ChangeUsername("admin", "root"); err != nil {
		t.Fatal(err)
	}
	if st.isDefaultPassword() {
		t.Error("non-admin username should not be flagged as default password")
	}
	if st.defaultPwdKnown.Load() != defaultPwdFalse {
		t.Errorf("verdict should be cached as false, got %d", st.defaultPwdKnown.Load())
	}
}
