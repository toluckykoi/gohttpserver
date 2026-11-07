package server

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// 第 8 轮性能审查回归：目录大小缓存预热、哈希缓存、访问配置 memo、SPA 外壳缓存。

func TestRound9MakeIndexPrepopulatesDirSizeCache(t *testing.T) {
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
	write("sub/a.txt", "12345")
	write("sub/deep/b.txt", "1234567")
	write("top.txt", "123")

	s := NewHTTPStaticServer(root, true) // noIndex=true：不起后台协程，手动触发
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}

	// 旧实现 makeIndex 后缓存为空，下一次列表需全树重走；现在应已预热。
	for dir, want := range map[string]int64{
		"sub":      12, // 5 + 7
		"sub/deep": 7,
		".":        15, // 5 + 7 + 3
	} {
		got, ok := dirInfoSize().peek(filepath.ToSlash(dir))
		if !ok {
			t.Errorf("dir size cache missing entry %q after makeIndex", dir)
			continue
		}
		if got != want {
			t.Errorf("dir size cache[%q] = %d, want %d", dir, got, want)
		}
	}
}

func TestRound9HashCacheHitAndInvalidation(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.bin")
	if err := os.WriteFile(p, []byte("CONTENT-VERSION-1"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	m5a, shaA := computeFileHashCached(p, fi)
	m5b, shaB := computeFileHashCached(p, fi)
	if m5a != m5b || shaA != shaB {
		t.Error("repeated call with unchanged file should return identical cached hashes")
	}
	if m5a == "" || shaA == "" {
		t.Fatal("hashes should not be empty")
	}

	// 同尺寸不同内容 + 明确推进 mtime：缓存必须按 (size, mtime) 失效。
	if err := os.WriteFile(p, []byte("CONTENT-VERSION-2"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatal(err)
	}
	fi2, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	m5c, _ := computeFileHashCached(p, fi2)
	if m5c == m5a {
		t.Error("cache served stale hash after content change with same size")
	}
}

func TestRound9AccessConfMemoMatchesReadAccessConf(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	conf := "accessTables:\n  - regex: '^secret'\n    allow: false\n"
	if err := os.WriteFile(filepath.Join(sub, ".ghs.yml"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)
	memo := newAccessConfMemo(s)

	ac := memo.get(sub)
	if ac.canAccess("secret.txt") {
		t.Error("memo result allowed a file the accessTables deny")
	}
	if !ac.canAccess("open.txt") {
		t.Error("memo result denied a file the accessTables allow")
	}
	if len(memo.memo) != 1 {
		t.Errorf("memo size = %d, want 1 (repeat lookups must be cached)", len(memo.memo))
	}
	direct := s.readAccessConf(sub)
	if direct.canAccess("secret.txt") != ac.canAccess("secret.txt") {
		t.Error("memo result diverges from readAccessConf")
	}
}

// fakeAssetFS 是仅含 index.html 的最小 http.FileSystem，用于验证外壳字节缓存。
type fakeAssetFS struct {
	content []byte
	opens   *atomic.Int32
}

func (f fakeAssetFS) Open(name string) (http.File, error) {
	f.opens.Add(1)
	if name != "index.html" && name != "/index.html" {
		return nil, os.ErrNotExist
	}
	return &fakeAssetFile{Reader: bytes.NewReader(f.content)}, nil
}

type fakeAssetFile struct {
	*bytes.Reader
}

func (f *fakeAssetFile) Close() error                       { return nil }
func (f *fakeAssetFile) Readdir(int) ([]os.FileInfo, error) { return nil, nil }
func (f *fakeAssetFile) Stat() (os.FileInfo, error)         { return fakeAssetInfo{}, nil }

type fakeAssetInfo struct{}

func (fakeAssetInfo) Name() string       { return "index.html" }
func (fakeAssetInfo) Size() int64        { return 0 }
func (fakeAssetInfo) Mode() os.FileMode  { return 0o444 }
func (fakeAssetInfo) ModTime() time.Time { return time.Time{} }
func (fakeAssetInfo) IsDir() bool        { return false }
func (fakeAssetInfo) Sys() any           { return nil }

var (
	_ http.File   = (*fakeAssetFile)(nil)
	_ os.FileInfo = fakeAssetInfo{}
	_ io.Closer   = (*fakeAssetFile)(nil)
)

func TestRound9SpaIndexHTMLCached(t *testing.T) {
	s := NewHTTPStaticServer(t.TempDir(), true)
	var opens atomic.Int32
	s.Assets = fakeAssetFS{content: []byte("<html>SPA</html>"), opens: &opens}

	first := s.spaIndexHTML()
	if first == nil || string(first) != "<html>SPA</html>" {
		t.Fatalf("spaIndexHTML = %q, want SPA content", first)
	}
	second := s.spaIndexHTML()
	if &first[0] != &second[0] {
		t.Error("spaIndexHTML should return the same cached bytes")
	}
	if n := opens.Load(); n != 1 {
		t.Errorf("Assets.Open called %d times, want 1 (shell bytes must be cached)", n)
	}
}
