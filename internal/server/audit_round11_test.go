package server

// 第 11 轮审计回归：M3（列表泄露受保护文件）与 M2（accessTables 正则大小写敏感）。

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// M3: 目录列表不得暴露服务器私有文件与访问控制配置本身，与搜索/下载通道对齐。
func TestListSkipsProtectedFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"gohttpserver.db":    "SECRETDB",
		"frp-encryption.key": "SECRETKEY",
		".ghs.yml":           "upload: false\n",
		"readme.txt":         "hello",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := NewHTTPStaticServer(root, true)
	req := httptest.NewRequest("GET", "/?json=true", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, name := range []string{"gohttpserver.db", "frp-encryption.key", ".ghs.yml"} {
		if strings.Contains(body, name) {
			t.Errorf("目录列表泄露受保护文件 %q（M3）", name)
		}
	}
	if !strings.Contains(body, "readme.txt") {
		t.Errorf("普通文件不应被过滤")
	}
}

// M2: Windows 文件系统大小写不敏感，规则与磁盘名大小写不一致时也必须隐藏/拦截。
func TestAccessTableRegexCaseInsensitive(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("M2 的大小写不敏感编译仅 Windows 启用（Linux 文件系统本身大小写敏感）")
	}
	root := t.TempDir()
	conf := "accessTables:\n- regex: secret\n  allow: false\n"
	if err := os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Secret.bin"), []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.bin"), []byte("fine"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)

	// 列表不得出现 Secret.bin（磁盘真实名与规则大小写不一致，此前直接显示）。
	req := httptest.NewRequest("GET", "/?json=true", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "Secret.bin") {
		t.Errorf("列表泄露大小写不一致的隐藏文件（M2）")
	}

	// 下载通道任意大小写都 404（此前 /Secret.bin 返回 200 完整内容）。
	for _, p := range []string{"/Secret.bin", "/secret.bin", "/SECRET.BIN"} {
		req := httptest.NewRequest("GET", p, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}

	// 不匹配规则的普通文件不受影响。
	req = httptest.NewRequest("GET", "/other.bin", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("GET /other.bin = %d, want 200", rec.Code)
	}
}
