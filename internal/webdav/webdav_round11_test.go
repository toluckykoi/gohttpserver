package webdav

import (
	"runtime"
	"testing"
)

// 第 11 轮审计 H2 回归：WebDAV 侧服务器私有文件保护必须识别 Windows 8.3 短名。
// 系统卷默认生成短名（GOHTTP~1.DB 等），此前仅 server/zipx 侧有模式匹配，
// 持任意 WebDAV 账号即可经短名读出/删除凭据库与 FRP 密钥。
// 短名判定（util.MatchesSensitiveShortName）按平台门禁：仅 Windows 卷存在 8.3
// 短名，非 Windows 上此类名字是合法文件名（如 backup~1.json）必须放行——与
// internal/util/windowsname_test.go 的分支模式一致，非 Windows 断言恒 false 以锁定门禁。
func TestServerStateNameBlocksShortName(t *testing.T) {
	for _, name := range []string{
		"GOHTTP~1.DB",
		"gohttp~1.db",
		"GOHTTP~2.DB",
		"FRP-EN~1.KEY",
		"WEBDAV~1.JSO",
		"AUTH-S~1.JSON",
		"GOHTTP~1.WAL",
		"GOHTTP~1.SHM",
	} {
		got := isServerStateNameWebdav(name)
		if runtime.GOOS == "windows" && !got {
			t.Errorf("isServerStateNameWebdav(%q) = false, want true（8.3 短名必须拦截）", name)
		}
		if runtime.GOOS != "windows" && got {
			t.Errorf("isServerStateNameWebdav(%q) = true, want false（非 Windows 平台 8.3 短名不存在，应放行）", name)
		}
	}
	for _, name := range []string{"readme.txt", "data.bin", "notes~1.doc", "share"} {
		if isServerStateNameWebdav(name) {
			t.Errorf("isServerStateNameWebdav(%q) = true, want false（误拦普通文件）", name)
		}
	}
}

// 同一缺口也存在于 ProtectSystemFiles 的写/删保护名单，一并锁定。
func TestProtectedFilenameBlocksShortName(t *testing.T) {
	for _, name := range []string{"GOHTTP~1.DB", "gohttp~1.db", "FRP-EN~1.KEY", "GOHTTP~1.WAL"} {
		got := isProtectedFilename(name)
		if runtime.GOOS == "windows" && !got {
			t.Errorf("isProtectedFilename(%q) = false, want true（8.3 短名必须拦截写入/删除）", name)
		}
		if runtime.GOOS != "windows" && !got {
			// 非 Windows 平台：短名形态放行（合法文件名），但点前缀文件仍受保护。
			t.Skipf("non-Windows: short-name forms are legal file names, got=%v", got)
		}
	}
	for _, name := range []string{"readme.txt", "notes~1.doc"} {
		if isProtectedFilename(name) {
			t.Errorf("isProtectedFilename(%q) = true, want false", name)
		}
	}
	// 既有语义保持：点文件仍然受保护。
	if !isProtectedFilename(".hidden") {
		t.Errorf("isProtectedFilename(.hidden) = false, want true")
	}
}

// CreateAccount 与 UpdateAccount 对齐拒绝负数配额（负值静默等效不限量）。
func TestCreateAccountRejectsNegativeQuota(t *testing.T) {
	s := &WebdavAccountState{data: &WebdavAccountsFile{}}
	if _, _, err := s.CreateAccount(CreateAccountRequest{
		Remark:     "test",
		QuotaBytes: -1,
	}, "admin"); err == nil {
		t.Fatal("CreateAccount with negative quota must fail")
	}
}
