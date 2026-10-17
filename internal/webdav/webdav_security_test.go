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
