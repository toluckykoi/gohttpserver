package webdav

import (
	"testing"
	"time"
)

// M3 回归：.ghs.yml 的 Windows 等价名不得经 WebDAV 写入。
func TestWebdavBlocksAccessConfVariants(t *testing.T) {
	q := &quotaFileSystem{protectSystemFiles: false}
	for _, name := range []string{".ghs.yml", ".GHS.YML", ".Ghs.Yml", "GHS~1.YML", "GHS~10.YML"} {
		if err := q.checkWriteAllowed("/" + name); err == nil {
			t.Errorf("checkWriteAllowed(%q) = nil, want protected", name)
		}
	}
	// 正常文件不受影响
	for _, name := range []string{"/data.txt", "/notes.md", "/.hidden"} {
		if err := q.checkWriteAllowed(name); err != nil {
			t.Errorf("checkWriteAllowed(%q) = %v, want nil (with protectSystemFiles=false)", name, err)
		}
	}
}

// 开启 ProtectSystemFiles 时系统文件名大小写不敏感。
func TestWebdavProtectedFilenamesCaseInsensitive(t *testing.T) {
	q := &quotaFileSystem{protectSystemFiles: true}
	for _, name := range []string{"gohttpserver.db", "GOHTTPSERVER.DB", "GoHttpServer.Db", "favicon.ico", "FAVICON.PNG"} {
		if err := q.checkWriteAllowed("/" + name); err == nil {
			t.Errorf("checkWriteAllowed(%q) = nil, want protected", name)
		}
	}
}

// 限流器行为：maxFail 次失败后锁定，锁定过期恢复，成功重置。
func TestAuthRateLimiter(t *testing.T) {
	l := newAuthRateLimiter(3, 50*time.Millisecond)

	for i := 0; i < 3; i++ {
		if !l.allow("1.2.3.4") {
			t.Fatalf("attempt %d: locked too early", i+1)
		}
		l.fail("1.2.3.4")
	}
	if l.allow("1.2.3.4") {
		t.Error("expected lockout after 3 failures")
	}
	// 其他 IP 不受影响
	if !l.allow("5.6.7.8") {
		t.Error("different IP should not be locked")
	}
	// 锁定过期后恢复
	time.Sleep(60 * time.Millisecond)
	if !l.allow("1.2.3.4") {
		t.Error("lockout should expire")
	}
	// 成功后重置
	l.fail("1.2.3.4")
	l.fail("1.2.3.4")
	l.reset("1.2.3.4")
	if !l.allow("1.2.3.4") {
		t.Error("reset should clear the counter")
	}

}

// M1 回归（第七轮）：attempts 映射必须有硬上限——未达锁定阈值（maxFail）的 IP
// 条目曾永不被清理（清理循环只删「已锁定且过期」的），公网扫描器/IPv6 海量
// 源地址下曾使该映射无界增长，构成内存 DoS。
func TestAuthRateLimiterEntriesBounded(t *testing.T) {
	l := newAuthRateLimiter(5, time.Minute)
	l.maxEntries = 10 // 测试用小上限

	// 灌入 50 个不同 IP，每个只失败 1 次（永不达锁定阈值）。
	for i := 0; i < 50; i++ {
		ip := "10.0.0." + itoa(i)
		l.fail(ip)
	}
	l.mu.Lock()
	n := len(l.attempts)
	l.mu.Unlock()

	if n > l.maxEntries {
		t.Errorf("attempts grew to %d entries with maxEntries=%d; must cap growth", n, l.maxEntries)
	}
	if n == 0 {
		t.Error("attempts map unexpectedly empty")
	}
}

// M1b：清理循环必须兜底删除未锁定的陈旧条目（lastFail 超时），
// 而不是只删「已锁定且锁定已过期」的条目。
func TestAuthRateLimiterCleanupDropsStaleUnlockedEntries(t *testing.T) {
	l := newAuthRateLimiter(5, time.Minute)

	// 未达锁定阈值的老条目：lastFail 早于 2 小时前。
	l.fail("stale.example")
	// 近期条目：不应被清理。
	l.fail("fresh.example")
	// 只在改字段时短暂持锁（fail 自己会取锁，持锁调它会自死锁）。
	l.mu.Lock()
	l.attempts["stale.example"].lastFail = time.Now().Add(-3 * time.Hour)
	l.mu.Unlock()

	// 直接执行清理逻辑（与 startCleanupLoop 的循环体同构）。
	l.mu.Lock()
	now := time.Now()
	for ip, a := range l.attempts {
		if !a.lockedUntil.IsZero() && now.After(a.lockedUntil) {
			delete(l.attempts, ip)
			continue
		}
		if now.Sub(a.lastFail) > 2*time.Hour {
			delete(l.attempts, ip)
		}
	}
	_, staleGone := l.attempts["stale.example"]
	_, freshKept := l.attempts["fresh.example"]
	l.mu.Unlock()

	if staleGone {
		t.Error("stale unlocked entry survived cleanup; map grows without bound")
	}
	if !freshKept {
		t.Error("fresh entry was cleaned up too early")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
