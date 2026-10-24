package server

// 第七轮审计修复的回归测试：H1（服务器私有文件可经写通道覆盖/删除）、
// H2（无扩展名 HTML 绕过 forcesAttachment 内联渲染）、H3（/-/unzip 内联 zip 内 HTML）、
// M2（resolvePath prefix 非边界剥离导致越界）。

import (
	"archive/zip"
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMultipartWriter 构造以 filename 同时作为 multipart 文件名的 writer，
// 覆盖 hUploadOrMkdir 的 header.Filename 分支（filename 字段另行单独设置）。
func newMultipartWriter(buf *bytes.Buffer, filename string) *multipart.Writer {
	mw := multipart.NewWriter(buf)
	return mw
}

// H1a: PUT 不得覆盖服务器私有文件（登录 DB、FRP 密钥）——曾可借此替换凭据库，
// 重启后以攻击者已知的哈希登录。
func TestEditRejectsServerStateFiles(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "gohttpserver.db")
	origDB := []byte("SQLITE-ORIGINAL-CONTENT")
	if err := os.WriteFile(dbPath, origDB, 0o644); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(root, "frp-encryption.key")
	if err := os.WriteFile(keyPath, []byte("ORIGINAL-KEY"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true

	for _, name := range []string{"gohttpserver.db", "GOHTTPSERVER.DB", "frp-encryption.key", "FRP-EN~1.KEY"} {
		req := httptest.NewRequest("PUT", "/"+name, strings.NewReader("ATTACKER CONTROLLED"))
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code == 200 {
			t.Errorf("PUT /%s: got 200, want rejection", name)
		}
	}
	if got, _ := os.ReadFile(dbPath); !bytes.Equal(got, origDB) {
		t.Errorf("PUT overwrote gohttpserver.db: now %q", got)
	}
}

// H1b: POST 上传不得覆盖服务器私有文件。
func TestUploadRejectsServerStateFiles(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "frp-encryption.key")
	if err := os.WriteFile(keyPath, []byte("ORIGINAL-KEY"), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true

	// multipart 文件名与 filename 字段各测一次：两者是独立代码路径。
	for _, filename := range []string{"gohttpserver.db", "frp-encryption.key", "GOHTTPSERVER.DB"} {
		var buf bytes.Buffer
		mw := newMultipartWriter(&buf, filename)
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write([]byte("ATTACKER"))
		mw.WriteField("filename", filename)
		mw.Close()

		req := httptest.NewRequest("POST", "/", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code == 200 {
			t.Errorf("POST upload filename=%q: got 200, want rejection", filename)
		}
	}
	if got, _ := os.ReadFile(keyPath); string(got) != "ORIGINAL-KEY" {
		t.Errorf("POST overwrote frp-encryption.key: now %q", got)
	}
}

// H1c: DELETE 不得删除服务器私有文件（直接删）或递归删除含私有文件的目录。
func TestDeleteRejectsServerStateFiles(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "gohttpserver.db")
	if err := os.WriteFile(dbPath, []byte("FAKE-BCRYPT-HASHES"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 目录内含私有文件：DELETE 该目录曾以 os.RemoveAll 连带删除 DB。
	sub := filepath.Join(root, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "gohttpserver.db"), []byte("NESTED-DB"), 0o644)
	os.WriteFile(filepath.Join(sub, "keep.txt"), []byte("keep"), 0o644)

	s := NewHTTPStaticServer(root, true)
	s.Delete = true

	req := httptest.NewRequest("DELETE", "/gohttpserver.db", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Error("DELETE /gohttpserver.db: got 200, want rejection")
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("DELETE removed gohttpserver.db")
	}

	// 递归删除含私有文件的目录必须被拒。
	req = httptest.NewRequest("DELETE", "/sub", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Error("DELETE /sub (contains gohttpserver.db): got 200, want rejection")
	}
	if _, err := os.Stat(filepath.Join(sub, "gohttpserver.db")); os.IsNotExist(err) {
		t.Error("DELETE /sub recursively removed the nested server state file")
	}
}

// H1d: name 校验层直接拒绝服务器私有文件名（与 .ghs.yml 同等级的 reserved name）。
func TestCheckFilenameRejectsServerStateNames(t *testing.T) {
	for _, name := range []string{
		"gohttpserver.db", "GOHTTPSERVER.DB", "gohttpserver.db-wal",
		"frp-encryption.key", "auth-state.json", "storage-usage.json",
	} {
		if err := checkFilename(name); err == nil {
			t.Errorf("checkFilename(%q) = nil, want reserved-file-name error", name)
		}
		if err := checkPathSegment(name); err == nil {
			t.Errorf("checkPathSegment(%q) = nil, want reserved-file-name error", name)
		}
	}
	// 正常文件名不受影响。
	for _, name := range []string{"normal.txt", "notes.md", "data.gHS.yml.bak"} {
		if err := checkFilename(name); err != nil {
			t.Errorf("checkFilename(%q) = %v, want nil", name, err)
		}
	}
}

// H2: 无扩展名 HTML 必须以 attachment 下发——http.ServeFile 的内容嗅探曾把它判为
// text/html 并内联渲染，扩展名黑名单无法覆盖此形态。
func TestExtensionlessHTMLForcesAttachment(t *testing.T) {
	root := t.TempDir()
	payload := []byte(`<html><head><title>x</title></head><body><script src="/evil.js"></script>STORED-XSS</body></html>`)
	if err := os.WriteFile(filepath.Join(root, "payload"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// 白名单扩展名的 HTML 内容（内容嗅探兜底）。
	if err := os.WriteFile(filepath.Join(root, "disguised.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "disguised.bak"), payload, 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)

	for _, name := range []string{"payload", "disguised.txt", "disguised.bak"} {
		req := httptest.NewRequest("GET", "/"+name, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		cd := rec.Header().Get("Content-Disposition")
		if !strings.Contains(cd, "attachment") {
			t.Errorf("GET /%s: Content-Disposition = %q, want attachment (CT=%q)",
				name, cd, rec.Header().Get("Content-Type"))
		}
	}
}

// H2b: 白名单扩展名的真实内容仍必须内联渲染（保护不误伤正常使用）。
func TestInlineSafeExtensionsStillRender(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("just text"), 0o644)
	os.WriteFile(filepath.Join(root, "b.png"), []byte("\x89PNG\r\n\x1a\n"), 0o644)
	os.WriteFile(filepath.Join(root, "c.mp4"), []byte("\x00\x00\x00\x18ftypmp42"), 0o644)
	os.WriteFile(filepath.Join(root, "d.json"), []byte(`{"ok":true}`), 0o644)

	s := NewHTTPStaticServer(root, true)
	for _, name := range []string{"a.txt", "b.png", "c.mp4", "d.json"} {
		req := httptest.NewRequest("GET", "/"+name, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if cd := rec.Header().Get("Content-Disposition"); strings.Contains(cd, "attachment") {
			t.Errorf("GET /%s: unexpectedly forced to attachment: %q", name, cd)
		}
	}
}

// H2c: 显式标记 unsafe 的扩展名（.svg/.xhtml/.swf）仍必须 attachment。
func TestExplicitlyUnsafeExtensionsForceAttachment(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "x.svg"), []byte(`<svg><script>alert(1)</script></svg>`), 0o644)
	os.WriteFile(filepath.Join(root, "y.xhtml"), []byte(`<html><body>x</body></html>`), 0o644)
	os.WriteFile(filepath.Join(root, "z.swf"), []byte("FWS\x08"), 0o644)

	s := NewHTTPStaticServer(root, true)
	for _, name := range []string{"x.svg", "y.xhtml", "z.swf"} {
		req := httptest.NewRequest("GET", "/"+name, nil)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
			t.Errorf("GET /%s: Content-Disposition = %q, want attachment", name, cd)
		}
	}
}

// H3: /-/unzip 抽取 zip 内条目必须以 attachment 下发——条目名带 .html 时经
// mime.TypeByExtension 被识别为 text/html 内联渲染，构成存储型 XSS 通道。
func TestUnzipEntryForcesAttachment(t *testing.T) {
	root := t.TempDir()
	payload := `<!DOCTYPE html><html><body><script src="/evil.js"></script>STORED-XSS-VIA-UNZIP</body></html>`
	zp := filepath.Join(root, "evil.zip")
	zf, _ := os.Create(zp)
	zw := zip.NewWriter(zf)
	fw, _ := zw.Create("x.html")
	fw.Write([]byte(payload))
	zw.Close()
	zf.Close()

	s := NewHTTPStaticServer(root, true)

	req := httptest.NewRequest("GET", "/-/unzip/evil.zip/-/x.html", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("unzip entry -> %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "STORED-XSS-VIA-UNZIP") {
		t.Error("unzip did not return the entry payload")
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Errorf("unzip entry Content-Disposition = %q, want attachment", cd)
	}
}

// M2a: resolvePath 的 prefix 剥离必须按路径段边界——"/shareX/leak.txt" 不得被
// 当成 "/share" 的子路径（曾由 filepath.Rel 产生 "../shareX/..." 而逃逸 Root）。
// 非边界前缀现在落回 Root 本身，因此断言"始终 insideRoot"即可。
func TestResolvePathPrefixBoundary(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "share-root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)
	s.Prefix = "/share"

	// 所有输入都必须在 Root 内：非边界前缀不得逃逸到 Root 之外（这正是漏洞所在）。
	for _, urlPath := range []string{
		"/share/a.txt", "/share/", "/share", "/share/sub/a.txt",
		"/shareX/a.txt", "/shared/a.txt", "/shareX/", "/shareXYZ",
	} {
		got := s.resolvePath(urlPath)
		if !s.insideRoot(got) {
			t.Errorf("resolvePath(%q) = %q escaped the root", urlPath, got)
		}
	}

	// 边界匹配的正常路径必须正确落位。
	if got := s.resolvePath("/share/a.txt"); filepath.ToSlash(got) != filepath.ToSlash(filepath.Join(root, "a.txt")) {
		t.Errorf("resolvePath(/share/a.txt) = %q, want %q", got, filepath.Join(root, "a.txt"))
	}
	if got := s.resolvePath("/share/sub/a.txt"); filepath.ToSlash(got) != filepath.ToSlash(filepath.Join(root, "sub", "a.txt")) {
		t.Errorf("resolvePath(/share/sub/a.txt) = %q, want %q", got, filepath.Join(root, "sub", "a.txt"))
	}
	// 非边界前缀落回根，而不是 Root 之外的 shareX 目录。
	leak := s.resolvePath("/shareX/a.txt")
	if strings.Contains(filepath.ToSlash(leak), "shareX") {
		t.Errorf("resolvePath(/shareX/a.txt) = %q, resolved outside the root (leaked sibling dir)", leak)
	}
}

// M2b: hIndex 是主文件读取通道，必须复核解析结果仍在 Root 内（其余读写通道已有复核）。
func TestIndexRejectsOutsideRootPath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "shareX")
	os.MkdirAll(root, 0o755)
	os.MkdirAll(outside, 0o755)
	os.WriteFile(filepath.Join(outside, "leak.txt"), []byte("OUTSIDE-THE-ROOT"), 0o644)

	s := NewHTTPStaticServer(root, true)
	s.Prefix = "/share"

	// 直接调 handler 绕过外层 mux 拓扑：曾经这样就能读到 Root 之外的文件。
	req := httptest.NewRequest("GET", "/shareX/leak.txt", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code == 200 && strings.Contains(rec.Body.String(), "OUTSIDE-THE-ROOT") {
		t.Errorf("GET /shareX/leak.txt leaked a file outside the root: %q", rec.Body.String())
	}
	if rec.Body.String() == "OUTSIDE-THE-ROOT" {
		t.Error("hIndex served a file outside the root verbatim")
	}
}

// M2c: stripPrefix 的段边界语义单元测试。
func TestStripPrefixSegmentBoundary(t *testing.T) {
	cases := []struct{ prefix, path, want string }{
		{"", "/a/b", "a/b"},
		{"", "/", ""},
		{"/share", "/share/a.txt", "a.txt"},
		{"/share", "/share", ""},
		{"/share", "/share/", ""},
		{"/share", "/shareX/a.txt", ""}, // 非边界：不得剥出
		{"/share", "/other/a.txt", ""},  // 不匹配：不得剥出
		{"/a/b", "/a/b/c", "c"},
		{"/a/b", "/a/bc", ""},
	}
	for _, tc := range cases {
		if got := stripPrefix(tc.prefix, tc.path); got != tc.want {
			t.Errorf("stripPrefix(%q, %q) = %q, want %q", tc.prefix, tc.path, got, tc.want)
		}
	}
}
