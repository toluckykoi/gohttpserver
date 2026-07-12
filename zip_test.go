package main

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractFromZip(t *testing.T) {
	// The fixture is not shipped in the repo (would bloat the git
	// history). Skip gracefully when absent so `go test ./...` stays
	// green in a fresh checkout; CI or local devs can drop the file in
	// to exercise the real path.
	if _, err := os.Stat("testdata/test.zip"); os.IsNotExist(err) {
		t.Skip("testdata/test.zip not present; skipping ExtractFromZip")
	}
	buf := bytes.NewBuffer(nil)
	err := ExtractFromZip("testdata/test.zip", "**/foo.txt", buf)
	assert.Nil(t, err)
	t.Log("Content: " + buf.String())
}

// TestUnzipFileRejectsZipSlip verifies the Zip Slip defence in unzipFile.
// A malicious zip whose entries use ".." to escape the destination must
// be rejected, and no file may be written outside dest. This is the
// regression test for the CVE-2018-1002200-class fix.
func TestUnzipFileRejectsZipSlip(t *testing.T) {
	// Build the malicious zip in a temp file so unzipFile (which takes a
	// path) can open it. Entries:
	//   - "safe.txt"           (should extract, proves the loop ran)
	//   - "../escape.txt"      (must be rejected)
	//   - "sub/../../oob.txt"  (must be rejected after Clean → ../oob.txt)
	zipPath := filepath.Join(t.TempDir(), "evil.zip")
	if err := writeZip(zipPath, []zipEntry{
		{Name: "safe.txt", Body: []byte("ok")},
		{Name: "../escape.txt", Body: []byte("leaked")},
		{Name: "sub/../../oob.txt", Body: []byte("leaked2")},
	}); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	dest := t.TempDir()
	err := unzipFile(context.Background(), zipPath, dest, nil)

	// The fix must reject the archive when it hits the escaping entry.
	if err == nil {
		t.Fatal("unzipFile accepted a zip with ../ entries (Zip Slip)")
	}
	if !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("unexpected error %q, want 'escapes destination'", err)
	}

	// safe.txt may or may not have been written before the offending
	// entry is reached (depends on zip entry order). Both are acceptable.
	// What MUST be true: neither escape file exists anywhere.
	if _, err := os.Stat(filepath.Join(dest, "..", "escape.txt")); err == nil {
		t.Fatal("escape.txt was written outside dest (Zip Slip succeeded)")
	}
	if _, err := os.Stat(filepath.Join(dest, "..", "oob.txt")); err == nil {
		t.Fatal("oob.txt was written outside dest (Zip Slip succeeded)")
	}
}

// TestUnzipFileRejectsAbsoluteEntry covers the rarer variant where a zip
// entry name is an absolute path (e.g. "/etc/passwd"). filepath.Join
// discards the leading separator, so this won't actually escape — but
// the test documents that behaviour and guards against regressions if
// the join logic ever changes.
func TestUnzipFileRejectsAbsoluteEntry(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "abs.zip")
	if err := writeZip(zipPath, []zipEntry{
		{Name: "/etc/passwd", Body: []byte("pwned")},
	}); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	dest := t.TempDir()
	err := unzipFile(context.Background(), zipPath, dest, nil)
	// filepath.Join("dest", "/etc/passwd") == "dest/etc/passwd", so this
	// does NOT escape and unzipFile should succeed. The test asserts
	// that behaviour explicitly: if a future change makes absolute
	// entries escape, this test will catch it.
	if err != nil {
		t.Fatalf("absolute entry unexpectedly rejected: %v", err)
	}
	// File should land inside dest (stripped of leading slash), not at /etc/passwd.
	got, err := os.ReadFile(filepath.Join(dest, "etc", "passwd"))
	if err != nil {
		t.Fatalf("expected dest/etc/passwd to exist: %v", err)
	}
	if string(got) != "pwned" {
		t.Fatalf("dest/etc/passwd content = %q, want %q", got, "pwned")
	}
}

type zipEntry struct {
	Name string
	Body []byte
}

// writeZip creates a zip archive at path containing the given entries.
// Helper for the Zip Slip tests so we don't need a binary fixture.
func writeZip(path string, entries []zipEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		w, err := zw.Create(e.Name)
		if err != nil {
			return err
		}
		if _, err := w.Write(e.Body); err != nil {
			return err
		}
	}
	return zw.Close()
}

//func TestUnzipTo(t *testing.T){
//	err := unzipFile("testdata.zip", "./tmp")
//	assert.Nil(t, err)
//}
