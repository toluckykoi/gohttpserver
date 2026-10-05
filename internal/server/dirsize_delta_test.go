package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Verifies the dir-size cache: index aggregation, delta updates on
// upload/delete, subtree drops, and the lazy walk fallback.
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

	s := NewHTTPStaticServer(root, true) // noIndex=true: no background loop
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}

	cases := map[string]int64{
		".":     175, // root
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

	// Upload delta: +60 into a/sub → a=210, a/sub=110, root=235
	mk("a/sub/new.bin", 60)
	dirInfoSize.addDelta("a/sub/new.bin", 60)
	if got := s.historyDirSize("a"); got != 210 {
		t.Errorf("after upload: a = %d, want 210", got)
	}
	if got := s.historyDirSize("."); got != 235 {
		t.Errorf("after upload: root = %d, want 235", got)
	}

	// Delete delta: remove a/sub (110 cached) → a=100, root=125
	dirInfoSize.addDelta("a/sub", -110)
	dirInfoSize.dropSubtree("a/sub")
	os.RemoveAll(filepath.Join(root, "a", "sub"))
	if got := s.historyDirSize("a"); got != 100 {
		t.Errorf("after delete: a = %d, want 100", got)
	}
	if got := s.historyDirSize("."); got != 125 {
		t.Errorf("after delete: root = %d, want 125", got)
	}

	// Rebuild from disk reflects reality exactly.
	if err := s.makeIndex(); err != nil {
		t.Fatal(err)
	}
	if got := s.historyDirSize("."); got != 125 {
		t.Errorf("after reindex: root = %d, want 125", got)
	}

	// Search still works and is case-insensitive via precomputed Lower.
	res := s.findIndex("F1")
	if len(res) != 1 || res[0].Path != "a/f1.txt" {
		t.Errorf("findIndex(F1) = %+v, want a/f1.txt", res)
	}
	// At this point a/sub (incl. f2.txt) has been deleted, so only
	// a/f1.txt and b/f3.txt remain; "txt -f3" matches just f1.
	res = s.findIndex("txt -f3")
	if len(res) != 1 || res[0].Path != "a/f1.txt" {
		t.Errorf("findIndex(txt -f3) = %+v, want only a/f1.txt", res)
	}
}

// Lazy fallback: with no index built at all (map nil), historyDirSize
// must still walk the subtree and return the correct size.
func TestDirSizeLazyFallback(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "x", "y")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p, "f.bin"), make([]byte, 42), 0o644)

	saved := dirInfoSize.size
	dirInfoSize.size = nil
	defer func() { dirInfoSize.size = saved }()

	s := NewHTTPStaticServer(root, true)
	if got := s.historyDirSize("x"); got != 42 {
		t.Errorf("lazy historyDirSize(x) = %d, want 42", got)
	}
}
