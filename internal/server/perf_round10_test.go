package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 第 10 轮性能审查回归：
//  1. 下载嗅探缓存按 (size, mtime) 失效，内容变化后判定必须更新。
//  2. 搜索分支逐结果访问配置 memo 化后，隐藏文件的过滤语义不变。
//  3. 目录列表直建切片后输出与原 map 路径一致，空结果仍序列化为 []。

func TestRound10SniffCacheInvalidatesOnContentChange(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "doc.txt")

	if err := os.WriteFile(p, []byte("innocent text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if looksLikeHTMLCached(p) {
		t.Fatal("plain text should not be flagged as HTML")
	}

	// 同尺寸换成 HTML 内容，并明确推进 mtime：缓存必须按 (size, mtime) 失效。
	html := []byte("<!doctype html>同等长度文本")
	if err := os.WriteFile(p, html, 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}
	if !looksLikeHTMLCached(p) {
		t.Error("cached verdict served stale result after content change with same size")
	}
}

func TestRound10SniffCacheFailsOpenOnStatError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.txt")
	// stat 失败时退回原实现（open 失败返回 false），不得 panic 或改判。
	if looksLikeHTMLCached(missing) {
		t.Error("missing file should not be flagged as HTML")
	}
}

// 搜索结果可能落在配置了更严 accessTables 的子目录：memo 化只是去重 IO，
// 过滤结果必须与逐条 canAccessPath 完全一致。
func TestRound10SearchBranchAccessFilterUnchanged(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("public/findme.txt", "public")
	write("private/findme.txt", "secret")
	write("private/other.txt", "other")
	write("private/.ghs.yml", "accessTables:\n- regex: '^findme'\n  allow: false\n")

	s := NewHTTPStaticServer(root, true)
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/?json=true&search=findme", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search request -> %d: %s", rec.Code, rec.Body.String())
	}

	var out struct {
		Files []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON: %v (%s)", err, rec.Body.String())
	}
	if len(out.Files) != 1 {
		t.Fatalf("got %d results %+v, want exactly the public hit", len(out.Files), out.Files)
	}
	if out.Files[0].Path != "public/findme.txt" {
		t.Errorf("hit path = %q, want public/findme.txt (private hit leaked or name wrong)", out.Files[0].Path)
	}
	if out.Files[0].Name != "public/findme.txt" {
		t.Errorf("hit name = %q, want relativized public/findme.txt", out.Files[0].Name)
	}
}

// 空目录列表必须输出 "files": []，不得退化为 null（非 nil 切片契约）。
func TestRound10EmptyListingJSONIsArray(t *testing.T) {
	s := NewHTTPStaticServer(t.TempDir(), true)

	req := httptest.NewRequest("GET", "/?json=true", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `"files":[]`) {
		t.Errorf("empty listing body = %s, want files:[]", body)
	}
}

// 目录列表输出与原 map 路径语义一致：名称、URL 路径键、类型、大小、目录深合并。
func TestRound10ListEntriesMatchExpectedShape(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("solo/a.txt", "12345") // 单子目录链：deepPath 应合并为 solo/a.txt
	write("top.bin", "1234567")

	s := NewHTTPStaticServer(root, true)

	req := httptest.NewRequest("GET", "/?json=true", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	var out struct {
		Files []HTTPFileInfo  `json:"files"`
		Auth  map[string]bool `json:"auth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("bad JSON: %v (%s)", err, rec.Body.String())
	}
	if len(out.Files) != 2 {
		t.Fatalf("got %d entries %+v, want 2", len(out.Files), out.Files)
	}
	byPath := map[string]HTTPFileInfo{}
	for _, f := range out.Files {
		byPath[f.Path] = f
	}
	solo, ok := byPath["solo"]
	if !ok {
		t.Fatalf("deep-merged entry solo missing: %+v", out.Files)
	}
	if solo.Type != "dir" || solo.Size != 5 || solo.Name != "solo" {
		t.Errorf("solo entry = type %q size %d name %q, want dir/5/solo", solo.Type, solo.Size, solo.Name)
	}
	top, ok := byPath["top.bin"]
	if !ok {
		t.Fatalf("file entry top.bin missing: %+v", out.Files)
	}
	if top.Type != "file" || top.Size != 7 || top.Name != "top.bin" {
		t.Errorf("top entry = %+v, want file/7/name top.bin", top)
	}
	if out.Auth["upload"] != s.Upload || out.Auth["delete"] != s.Delete || out.Auth["edit"] != s.Edit {
		t.Errorf("auth flags = %v, want defaults upload=%v delete=%v edit=%v",
			out.Auth, s.Upload, s.Delete, s.Edit)
	}
}
