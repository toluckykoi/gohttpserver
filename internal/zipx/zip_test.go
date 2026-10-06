package zipx

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// CompressToZip used to dereference a nil os.FileInfo: filepath.Walk calls the
// callback with info == nil when the root cannot be lstat'ed, so archiving a
// path that does not exist panicked the request (GET /missing?op=archive).
func TestCompressToZipMissingPathDoesNotPanic(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	rec := httptest.NewRecorder()

	CompressToZip(rec, missing) // must not panic

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("output is not a readable zip: %v", err)
	}
	if len(zr.File) != 0 {
		t.Errorf("got %d entries for a missing root, want 0", len(zr.File))
	}
}

// Archiving a single file must name the entry after that file. It used to be
// named "." because the relative path for the walk root is empty.
func TestCompressToZipSingleFileEntryName(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "report.txt")
	content := []byte("hello archive")
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	CompressToZip(rec, target)

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("output is not a readable zip: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("got %d entries, want 1", len(zr.File))
	}
	if got := zr.File[0].Name; got != "report.txt" {
		t.Errorf("entry name = %q, want %q", got, "report.txt")
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("entry content = %q, want %q", got, content)
	}
}

// A directory archive must skip .ghs.yml: its body holds the per-user tokens.
func TestCompressToZipSkipsAccessControlFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ghs.yml"), []byte("token: SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	CompressToZip(rec, dir)

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("output is not a readable zip: %v", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == ".ghs.yml" {
			t.Fatalf("archive contains the access-control file: %q", f.Name)
		}
	}
	if len(zr.File) == 0 {
		t.Error("archive is empty, expected the visible file")
	}
}
