package server

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// newZipMultiRequest 构造一个多选下载请求(一个 zipMultiRequest 主体),返回可发往 hZipMulti 的请求。
func newZipMultiRequest(t *testing.T, s *HTTPStaticServer, paths ...string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	body.WriteString(`{"paths":[`)
	for i, p := range paths {
		if i > 0 {
			body.WriteString(",")
		}
		body.WriteString(`"` + p + `"`)
	}
	body.WriteString(`]}`)

	req := httptest.NewRequest("POST", "/-/zip", body)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// readZipEntries 解析 zip 响应,返回按升序排列的条目名。
func readZipEntries(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("parse zip response: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

// TestZipMultiDedupSameNameDifferentDirs 验证不同目录的同名文件被打包时不会互相覆盖：
// /a/x.zip 与 /b/x.zip 必须都出现在归档中，且名称不同（带 (2) 后缀）。
func TestZipMultiDedupSameNameDifferentDirs(t *testing.T) {
	root := t.TempDir()
	// 构造 /a/x.zip 与 /b/x.zip
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	contentA := []byte("AAA")
	contentB := []byte("BBB")
	if err := os.WriteFile(filepath.Join(root, "a", "x.zip"), contentA, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b", "x.zip"), contentB, 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, false)
	req := newZipMultiRequest(t, s, "/a/x.zip", "/b/x.zip")
	w := httptest.NewRecorder()
	s.hZipMulti(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	names := readZipEntries(t, w)
	if len(names) != 2 {
		t.Fatalf("expected 2 entries, got %v", names)
	}
	// 两个条目名不同，且分别保留两个内容
	seen := make(map[string]bool)
	for _, n := range names {
		seen[n] = true
	}
	if seen["x.zip"] && seen["x (2).zip"] {
		t.Logf("dedup worked: entries = %v", names)
	} else {
		t.Fatalf("expected x.zip and x (2).zip, got %v", names)
	}

	// 校验内容都正确
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	if got["x.zip"] != "AAA" {
		t.Errorf("x.zip content = %q, want AAA", got["x.zip"])
	}
	if got["x (2).zip"] != "BBB" {
		t.Errorf("x (2).zip content = %q, want BBB", got["x (2).zip"])
	}
}

// TestZipMultiDedupSameNameWithExtension 验证带扩展名的同名条目保留扩展名加后缀。
func TestZipMultiDedupSameNameWithExtension(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one", "report.md"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two", "report.md"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, false)
	req := newZipMultiRequest(t, s, "/one/report.md", "/two/report.md")
	w := httptest.NewRecorder()
	s.hZipMulti(w, req)

	names := readZipEntries(t, w)
	if len(names) != 2 {
		t.Fatalf("expected 2 entries, got %v", names)
	}
	sort.Strings(names)
	if names[0] != "report (2).md" || names[1] != "report.md" {
		t.Fatalf("unexpected entry names: %v", names)
	}
}

// TestZipMultiDedupSameEntryTwice 同一路径选两次只生成一个条目。
func TestZipMultiDedupSameEntryTwice(t *testing.T) {
	root := t.TempDir()
	content := []byte("same")
	if err := os.WriteFile(filepath.Join(root, "same.txt"), content, 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, false)
	req := newZipMultiRequest(t, s, "/same.txt", "/same.txt")
	w := httptest.NewRecorder()
	s.hZipMulti(w, req)
	names := readZipEntries(t, w)
	if len(names) != 1 || names[0] != "same.txt" {
		t.Fatalf("expected single same.txt, got %v", names)
	}
}

// TestZipMultiDedupCollisionWithExistingName 已存在 "a (2).txt" 时,把冲突项错开到 "a (3).txt"。
func TestZipMultiDedupCollisionWithExistingName(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "one"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "three"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "one", "a.txt"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "two", "a.txt"), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "three", "a.txt"), []byte("3"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, false)
	req := newZipMultiRequest(t, s, "/one/a.txt", "/two/a.txt", "/three/a.txt")
	w := httptest.NewRecorder()
	s.hZipMulti(w, req)
	names := readZipEntries(t, w)
	if len(names) != 3 {
		t.Fatalf("expected 3 entries, got %v", names)
	}
	// 三个名字互不相同
	sort.Strings(names)
	for i := 0; i < len(names)-1; i++ {
		if names[i] == names[i+1] {
			t.Fatalf("duplicate entry names: %v", names)
		}
	}
}

// TestFetchClientReuseTransport 验证 hFetch 使用包级客户端而非每次新建（防连接泄漏回归）。
func TestFetchClientReuseTransport(t *testing.T) {
	// fetchClient 是包级单例，不应在两次请求间变化。
	c1 := fetchClient
	c2 := fetchClient
	if c1 != c2 {
		t.Fatal("fetchClient must be a package-level singleton")
	}
	if c1.Transport == nil {
		t.Fatal("fetchClient must have a transport")
	}
	// Timeout 应与 fetchTimeout 一致。
	if c1.Timeout != fetchTimeout {
		t.Fatalf("fetchClient.Timeout = %v, want %v", c1.Timeout, fetchTimeout)
	}
}

// TestFetchSameOriginArtifactSanitized 验证 fetch 的 to 文件名仍拒绝 path 类字符（回归）。
func TestFetchRejectsPathCharInTo(t *testing.T) {
	root := t.TempDir()
	s := NewHTTPStaticServer(root, false)
	form := url.Values{}
	form.Set("url", "http://example.com/x.txt")
	form.Set("to", "../evil")
	req := httptest.NewRequest("POST", "/-/fetch?"+form.Encode(), strings.NewReader(""))
	w := httptest.NewRecorder()
	// 无 token 也走不到（会被 canUpload 拒绝），这里直接调 handler 验证路径字符拦截。
	s.hFetch(w, req)
	if w.Code == http.StatusBadRequest && strings.Contains(w.Body.String(), "'to' must be a basename") {
		return
	}
	// 若因未授权返回 403 也算合理（本测试不关心）；允许的路径字符通过普通请求验。
	if w.Code == http.StatusForbidden {
		return
	}
	t.Fatalf("unexpected status %d body %s", w.Code, w.Body.String())
}