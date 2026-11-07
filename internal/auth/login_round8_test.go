package auth

import (
	"testing"
)

// 第 8 轮审计回归。

func TestRound8LoginWhitelistSegmentBoundary(t *testing.T) {
	cases := map[string]bool{
		"/-/login":                true,
		"/-/logout":               true,
		"/-/user":                 true,
		"/-/sysinfo":              true,
		"/-/api/auth/status":      true,
		"/-/frontend/":            true,
		"/-/frontend/assets/x.js": true,
		"/dav":                    true,
		"/dav/":                   true,
		"/dav/sub/file.txt":       true,
		// 旧实现用无边界 HasPrefix，以下前缀穿透路径曾全部放行。
		"/-/loginX":           false,
		"/-/logout-and-then":  false,
		"/-/userX":            false,
		"/-/sysinfoX":         false,
		"/-/api/auth/statusX": false,
		"/-/frontendX":        false,
		"/favicon.icoX":       false,
	}
	for path, want := range cases {
		if got := isLoginWhitelisted(path); got != want {
			t.Errorf("isLoginWhitelisted(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestRound8LoginRateLimiterEvictsInsteadOfFailOpen(t *testing.T) {
	st := LoadLoginCredentials(nil) // nil db 只影响凭据，不影响限流器
	newIP := "203.0.113.99"
	for i := 0; i < st.maxAttemptEntries; i++ {
		st.recordFailedLogin(fakeLoginIP(i))
	}
	st.rateLimitMu.Lock()
	size := len(st.loginAttempts)
	st.rateLimitMu.Unlock()
	if size != st.maxAttemptEntries {
		t.Fatalf("map size %d after fill, want %d", size, st.maxAttemptEntries)
	}

	// 旧实现满时直接 return，新 IP 的失败不被记录，checkRateLimit 对其恒放行。
	st.recordFailedLogin(newIP)
	st.rateLimitMu.Lock()
	_, recorded := st.loginAttempts[newIP]
	size = len(st.loginAttempts)
	st.rateLimitMu.Unlock()
	if !recorded {
		t.Error("new IP failure not recorded when map is full (fail-open regression)")
	}
	if size > st.maxAttemptEntries {
		t.Errorf("map size %d exceeds hard cap", size)
	}
	// 已记录的 IP 达到阈值仍应触发锁定。
	for i := 0; i < st.maxAttempts; i++ {
		st.recordFailedLogin(newIP)
	}
	if st.checkRateLimit(newIP) {
		t.Error("new IP reached lockout threshold but checkRateLimit still allows")
	}
}

func fakeLoginIP(i int) string {
	return "198.51.100." + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26))
}

func TestRound8PasswordPolicy(t *testing.T) {
	if err := ValidatePassword("abc"); err == nil {
		t.Error("4-char password must be rejected now")
	}
	if err := ValidatePassword("12345678"); err != nil {
		t.Errorf("8-char password should pass: %v", err)
	}
	long := make([]byte, 73)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidatePassword(string(long)); err == nil {
		t.Error("73-byte password must be rejected (bcrypt truncation)")
	}
}
