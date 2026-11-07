package zipx

import (
	"archive/zip"
	"bytes"
	"io"
	"math/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 第 10 轮性能审查回归：
//  1. 明显不可压缩类型（媒体/压缩容器）用 Store，其余保持 Deflate。
//  2. zip 写出经 bufio 缓冲后 central directory 完整、归档可正常解出。
//  3. AddWithInfo（复用 walk 的 FileInfo）与 Add（自行 lstat）产出一致。

func TestRound10ZipMethodSelection(t *testing.T) {
	dir := t.TempDir()
	media := make([]byte, 4096)
	rand.New(rand.NewSource(1)).Read(media) // 高熵字节，模拟媒体内容
	if err := os.WriteFile(filepath.Join(dir, "clip.mp4"), media, 0o644); err != nil {
		t.Fatal(err)
	}
	text := []byte(strings.Repeat("hello zip buffer ", 256))
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), text, 0o644); err != nil {
		t.Fatal(err)
	}
	nested := make([]byte, 2048)
	rand.New(rand.NewSource(2)).Read(nested)
	if err := os.WriteFile(filepath.Join(dir, "nested.bin"), nested, 0o644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	CompressToZipFiltered(rec, dir, nil)

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("output is not a readable zip: %v", err)
	}
	// 归档根目录自身也会成条目（stdlib 对目录条目强制 Store，与旧实现一致），
	// 因此共 4 条：根目录 + 3 个文件。
	want := map[string]uint16{
		"clip.mp4":   zip.Store,
		"notes.txt":  zip.Deflate,
		"nested.bin": zip.Deflate,
	}
	if len(zr.File) != len(want)+1 {
		t.Fatalf("got %d entries, want %d (3 files + root dir)", len(zr.File), len(want)+1)
	}
	seen := map[string]bool{}
	for _, f := range zr.File {
		if m, ok := want[f.Name]; ok {
			if f.Method != m {
				t.Errorf("entry %q method = %d, want %d", f.Name, f.Method, m)
			}
		} else if !f.FileInfo().IsDir() {
			t.Errorf("unexpected entry %q", f.Name)
		}
		seen[f.Name] = true

		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %q: %v", f.Name, err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read %q: %v", f.Name, err)
		}
		switch f.Name {
		case "clip.mp4":
			if !bytes.Equal(got, media) {
				t.Errorf("clip.mp4 content corrupted in buffered writer path")
			}
		case "notes.txt":
			if !bytes.Equal(got, text) {
				t.Errorf("notes.txt content corrupted in buffered writer path")
			}
		case "nested.bin":
			if !bytes.Equal(got, nested) {
				t.Errorf("nested.bin content corrupted in buffered writer path")
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("missing entry %q", name)
		}
	}
}

// AddWithInfo 传入 walk 的 FileInfo 后，条目头（名字/大小/mtime/方法）与内容
// 必须与 Add（自行 lstat）完全一致。
func TestRound10AddWithInfoMatchesAdd(t *testing.T) {
	dir := t.TempDir()
	content := []byte("identical via both add paths")
	src := filepath.Join(dir, "same.txt")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(src)
	if err != nil {
		t.Fatal(err)
	}

	var viaAdd, viaInfo bytes.Buffer
	za, flushA := NewBufferedZip(&viaAdd)
	if err := za.Add("a.txt", src); err != nil {
		t.Fatal(err)
	}
	if err := za.Close(); err != nil {
		t.Fatal(err)
	}
	flushA()

	zi, flushI := NewBufferedZip(&viaInfo)
	if err := zi.AddWithInfo("a.txt", src, info); err != nil {
		t.Fatal(err)
	}
	if err := zi.Close(); err != nil {
		t.Fatal(err)
	}
	flushI()

	readEntry := func(t *testing.T, data []byte) (*zip.FileHeader, []byte) {
		t.Helper()
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("unreadable zip: %v", err)
		}
		if len(zr.File) != 1 {
			t.Fatalf("got %d entries, want 1", len(zr.File))
		}
		f := zr.File[0]
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		body, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return &f.FileHeader, body
	}

	hA, bodyA := readEntry(t, viaAdd.Bytes())
	hI, bodyI := readEntry(t, viaInfo.Bytes())
	if hA.Name != hI.Name || hA.UncompressedSize64 != hI.UncompressedSize64 || !hA.Modified.Equal(hI.Modified) || hA.Method != hI.Method {
		t.Errorf("header mismatch: Add=%+v AddWithInfo=%+v", *hA, *hI)
	}
	if !bytes.Equal(bodyA, bodyI) || !bytes.Equal(bodyI, content) {
		t.Error("entry content mismatch between Add and AddWithInfo")
	}
}

// zipMethodFor 单元表：不可压缩扩展名命中 Store，未知/可压扩展名保持 Deflate。
func TestRound10ZipMethodFor(t *testing.T) {
	for name, want := range map[string]uint16{
		"movie.MP4":   zip.Store,
		"backup.ZIP":  zip.Store,
		"photo.jpg":   zip.Store,
		"song.flac":   zip.Store,
		"pkg.docx":    zip.Store,
		"notes.txt":   zip.Deflate,
		"main.go":     zip.Deflate,
		"data.csv":    zip.Deflate,
		"noextension": zip.Deflate,
		"doc.pdf":     zip.Deflate,
	} {
		if got := zipMethodFor(name); got != want {
			t.Errorf("zipMethodFor(%q) = %d, want %d", name, got, want)
		}
	}
}
