package server

import (
	"os"
	"path/filepath"
	"testing"
)

// 验证目录大小缓存：索引聚合、上传/删除的增量更新、子树删除与惰性遍历回退。
func TestDirSizeCacheAggregationAndDeltas(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, size int) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a/f1.txt", 100)
	mk("a/sub/f2.txt", 50)
	mk("b/f3.txt", 25)
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true) // noIndex=true：不起后台循环
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}

	cases := map[string]int64{
		".":     175, // 根目录
		"a":     150,
		"a/sub": 50,
		"b":     25,
		"empty": 0,
	}
	for dir, want := range cases {
		if got := s.historyDirSize(dir); got != want {
			t.Errorf("historyDirSize(%q) = %d, want %d", dir, got, want)
		}
	}

	// 上传增量：向 a/sub 写入 +60 → a=210、a/sub=110、root=235
	mk("a/sub/new.bin", 60)
	dirInfoSize.addDelta("a/sub/new.bin", 60)
	if got := s.historyDirSize("a"); got != 210 {
		t.Errorf("after upload: a = %d, want 210", got)
	}
	if got := s.historyDirSize("."); got != 235 {
		t.Errorf("after upload: root = %d, want 235", got)
	}

	// 删除增量：移除 a/sub（缓存 110）→ a=100、root=125
	dirInfoSize.addDelta("a/sub", -110)
	dirInfoSize.dropSubtree("a/sub")
	os.RemoveAll(filepath.Join(root, "a", "sub"))
	if got := s.historyDirSize("a"); got != 100 {
		t.Errorf("after delete: a = %d, want 100", got)
	}
	if got := s.historyDirSize("."); got != 125 {
		t.Errorf("after delete: root = %d, want 125", got)
	}

	// 从磁盘重建后与实际情况完全一致。
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}
	if got := s.historyDirSize("."); got != 125 {
		t.Errorf("after reindex: root = %d, want 125", got)
	}

	// 搜索仍可用，并借助预计算的 Lower 实现大小写不敏感。
	res := s.findIndex("F1")
	if len(res) != 1 || res[0].Path != "a/f1.txt" {
		t.Errorf("findIndex(F1) = %+v, want a/f1.txt", res)
	}
	// 此时 a/sub（含 f2.txt）已删除，只剩 a/f1.txt 与 b/f3.txt；"txt -f3" 只命中 f1。
	res = s.findIndex("txt -f3")
	if len(res) != 1 || res[0].Path != "a/f1.txt" {
		t.Errorf("findIndex(txt -f3) = %+v, want only a/f1.txt", res)
	}
}

// 惰性回退：完全没有索引（map 为 nil）时，historyDirSize 仍须遍历子树并返回正确大小。
func TestDirSizeLazyFallback(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x", "y")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p, "f.bin"), make([]byte, 42), 0o644)

	// 用全新的空分片缓存替换
	saved := dirInfoSize
	dirInfoSize = shardedDirSize{}
	defer func() { dirInfoSize = saved }()

	s := NewHTTPStaticServer(root, true)
	if got := s.historyDirSize("x"); got != 42 {
		t.Errorf("lazy historyDirSize(x) = %d, want 42", got)
	}
}
