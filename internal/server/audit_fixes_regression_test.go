package server

// 第六轮审计修复的回归测试：H1（zip 泄露 .ghs.yml）、H2（info 泄露元数据/哈希）、
// H3（服务器私有文件可下载）、M1（搜索绕过 accessTables）、M3（unzip 绕过 accessTables）。

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codeskyblue/gohttpserver/internal/zipx"
)

// auditZipEntries 把 rec 响应体解析为 zip 并返回 条目名→内容 映射。
func auditZipEntries(t *testing.T, body []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a valid zip: %v", err)
	}
	out := make(map[string]string)
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry: %v", err)
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()
		out[f.Name] = buf.String()
	}
	return out
}

// H1: POST /-/zip 单文件路径不得导出 .ghs.yml（曾泄露 users[].token）。
func TestZipMultiBlocksAccessConfSingleFile(t *testing.T) {
	root := t.TempDir()
	conf := "upload: false\ndelete: false\nusers:\n- email: a@b.c\n  token: SUPERSECRET123\n"
	if err := os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)

	// 大小写变体在 Windows 上与同一文件对应，同样必须拒绝。
	for _, p := range []string{"/.ghs.yml", "/.GHS.YML"} {
		req := httptest.NewRequest("POST", "/-/zip", strings.NewReader(`{"paths":["`+p+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		for name, content := range auditZipEntries(t, rec.Body.Bytes()) {
			if strings.Contains(content, "SUPERSECRET123") {
				t.Errorf("POST /-/zip path %q: entry %q leaked access conf token", p, name)
			}
		}
	}
}

// H1b: POST /-/zip 单文件路径不得导出服务器私有文件（登录 DB、FRP 密钥）。
func TestZipMultiBlocksServerStateFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("FAKE-BCRYPT-HASHES"), 0o644)
	os.WriteFile(filepath.Join(root, "frp-encryption.key"), []byte("0123456789abcdef0123456789abcdef"), 0o600)
	s := NewHTTPStaticServer(root, true)

	for _, p := range []string{"/gohttpserver.db", "/GOHTTPSERVER.DB", "/frp-encryption.key"} {
		req := httptest.NewRequest("POST", "/-/zip", strings.NewReader(`{"paths":["`+p+`"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		for name, content := range auditZipEntries(t, rec.Body.Bytes()) {
			if strings.Contains(content, "FAKE-BCRYPT") || strings.Contains(content, "0123456789abcdef") {
				t.Errorf("POST /-/zip path %q: entry %q leaked server state file", p, name)
			}
		}
	}
}

// H1c: 目录打包的 walk 分支同样要跳过服务器私有文件。
func TestZipMultiWalkSkipsServerStateFiles(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "gohttpserver.db"), []byte("FAKE-BCRYPT-HASHES"), 0o644)
	os.WriteFile(filepath.Join(root, "sub", "keep.txt"), []byte("keep"), 0o644)
	s := NewHTTPStaticServer(root, true)

	req := httptest.NewRequest("POST", "/-/zip", strings.NewReader(`{"paths":["/sub"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	for name, content := range auditZipEntries(t, rec.Body.Bytes()) {
		if strings.Contains(name, "gohttpserver.db") && strings.Contains(content, "FAKE-BCRYPT") {
			t.Errorf("directory zip leaked server state file: %s", name)
		}
	}
}

// H2: ?op=info 与 /-/info/ 对 .ghs.yml 与服务器私有文件一律 404（曾泄露 md5/sha256）。
func TestInfoBlocksAccessConfAndStateFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte("token: SECRET2\n"), 0o644)
	os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("FAKE-BCRYPT-HASHES"), 0o644)
	s := NewHTTPStaticServer(root, true)

	for _, url := range []string{
		"/.ghs.yml?op=info",
		"/-/info/.ghs.yml",
		"/gohttpserver.db?op=info",
		"/-/info/gohttpserver.db",
	} {
		req := httptest.NewRequest("GET", url, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code == 200 {
			t.Errorf("GET %s: got 200 with body %s, want 404", url, rec.Body.String())
		}
	}
}

// H3: GET 不得下载服务器私有文件（含大小写与 8.3 短名形式）。
func TestRawBlocksServerStateFiles(t *testing.T) {
	if !isServerStateName("GOHTTP~1.DB") {
		t.Fatalf("precondition: isServerStateName must recognize 8.3 short name GOHTTP~1.DB")
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("FAKE-BCRYPT-HASHES"), 0o644)
	os.WriteFile(filepath.Join(root, "frp-encryption.key"), []byte("0123456789abcdef0123456789abcdef"), 0o600)
	s := NewHTTPStaticServer(root, true)

	for _, url := range []string{"/gohttpserver.db", "/GOHTTPSERVER.DB", "/frp-encryption.key", "/FRP-EN~1.KEY"} {
		req := httptest.NewRequest("GET", url, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code == 200 && (strings.Contains(rec.Body.String(), "FAKE-BCRYPT") || rec.Body.Len() == 32) {
			t.Errorf("GET %s: leaked server state file (status %d, %d bytes)", url, rec.Code, rec.Body.Len())
		}
	}
}

// M1: 搜索不得泄露子目录 accessTables 隐藏的文件名。
func TestSearchRespectsAccessTablesOfHitDirectory(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "secret-plan.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "public-plan.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(sub, ".ghs.yml"), []byte("accessTables:\n- regex: secret\n  allow: false\n"), 0o644)
	s := NewHTTPStaticServer(root, true)
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/?json=true&search=plan", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("search failed: %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret-plan") {
		t.Errorf("search leaked accessTables-hidden file: %s", body)
	}
	if !strings.Contains(body, "public-plan") {
		t.Errorf("search should still return visible files, got: %s", body)
	}
}

// M1b: 搜索同样不得泄露 .ghs.yml 与服务器私有文件名。
func TestSearchSkipsProtectedFiles(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte("token: x\n"), 0o644)
	os.WriteFile(filepath.Join(root, "gohttpserver.db"), []byte("FAKE"), 0o644)
	s := NewHTTPStaticServer(root, true)
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/?json=true&search=", nil)
	req.URL.Query().Set("search", "")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	// search 只在非空时走索引分支；用常见词触发。
	req2 := httptest.NewRequest("GET", "/?json=true&search=gohttpserver", nil)
	rec2 := httptest.NewRecorder()
	s.ServeHTTP(rec2, req2)
	if strings.Contains(rec2.Body.String(), "gohttpserver.db") {
		t.Errorf("search leaked server state file name: %s", rec2.Body.String())
	}
}

// M3: /-/unzip 不得抽取 accessTables 隐藏的 zip 的条目。
func TestUnzipBlocksHiddenZip(t *testing.T) {
	root := t.TempDir()
	// 构造真实 zip 供 ExtractFromZip 打开。
	zp := filepath.Join(root, "hidden.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("payload.txt")
	w.Write([]byte("TOP-SECRET-PAYLOAD"))
	zw.Close()
	zf.Close()

	os.WriteFile(filepath.Join(root, ".ghs.yml"), []byte("accessTables:\n- regex: hidden\n  allow: false\n"), 0o644)
	s := NewHTTPStaticServer(root, true)

	req := httptest.NewRequest("GET", "/-/unzip/hidden.zip/-/payload.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == 200 && strings.Contains(rec.Body.String(), "TOP-SECRET-PAYLOAD") {
		t.Errorf("/-/unzip extracted entries from accessTables-hidden zip")
	}
}

// M2 修复：zip 解压在 Windows 上不得落盘大小写/短名变体的 .ghs.yml。
func TestUnzipFileRejectsAccessConfVariants(t *testing.T) {
	entries := []string{".GHS.YML", ".ghs.yml.", "GHS~1.YML", ".GHS.YAML"}
	for _, name := range entries {
		root := t.TempDir()
		zp := filepath.Join(root, "evil.zip")
		zf, _ := os.Create(zp)
		zw := zip.NewWriter(zf)
		w, _ := zw.Create(name)
		w.Write([]byte("delete: true\nedit: true\n"))
		zw.Close()
		zf.Close()

		dest := filepath.Join(root, "out")
		os.MkdirAll(dest, 0o755)
		if err := zipx.UnzipFile(context.Background(), zp, dest, nil); err != nil {
			t.Fatalf("unzip %q: %v", name, err)
		}
		// Windows 不区分大小写：任何 .ghs.yml 等价名落盘都会被 readAccessConf 读到。
		if _, err := os.Stat(filepath.Join(dest, ".ghs.yml")); err == nil {
			t.Errorf("zip entry %q was extracted to disk as access conf (privilege escalation vector)", name)
		}
	}
}

// 正常 .ghs.yml 之外的普通文件仍可解压（保护不误伤）。
func TestUnzipFileStillExtractsNormalFiles(t *testing.T) {
	root := t.TempDir()
	zp := filepath.Join(root, "ok.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	w, _ := zw.Create("notes.gHS.yml.bak")
	w.Write([]byte("backup"))
	w2, _ := zw.Create("regular.txt")
	w2.Write([]byte("regular"))
	zw.Close()
	zf.Close()

	dest := filepath.Join(root, "out")
	os.MkdirAll(dest, 0o755)
	if err := zipx.UnzipFile(context.Background(), zp, dest, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "regular.txt")); err != nil {
		t.Errorf("normal file was blocked from extraction: %v", err)
	}
}
