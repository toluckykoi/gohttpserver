package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression test for a Windows-only remote DoS: gorilla/mux normalises the
// request path with path.Clean (slash-only), but filepath.Clean on Windows also
// treats "\" as a separator. A request like POST /..%5C..%5C therefore survived
// routing, resolvePath computed a path ABOVE Root, and readAccessConf recursed
// until the process died with an unrecoverable stack overflow. ServeHTTP now
// rejects any backslash path outright.
func TestBackslashPathsAreRejected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Upload, s.Delete, s.Edit = true, true, true

	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		// Literal backslashes (some clients send them raw).
		for _, p := range []string{
			`/..\..\Windows\System32\drivers\etc`,
			`/sub\..\..\escape.txt`,
		} {
			req := httptest.NewRequest(method, "/", nil)
			req.URL.Path = p
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s %q: got status %d, want 400", method, p, rec.Code)
			}
		}

		// Percent-encoded backslashes, which net/http decodes into URL.Path
		// before any of our code sees them. Building the request through
		// httptest.NewRequest (rather than assigning URL.Path) is what makes
		// this exercise the same path a real client would take.
		req := httptest.NewRequest(method, "/..%5C..%5C", nil)
		if !strings.ContainsRune(req.URL.Path, '\\') {
			t.Fatalf("precondition failed: %%5C was not decoded, URL.Path = %q", req.URL.Path)
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s /..%%5C..%%5C (decoded %q): got status %d, want 400", method, req.URL.Path, rec.Code)
		}
	}
}

// readAccessConf must terminate (and return the defaults) for a path that is
// not under Root. It used to recurse forever when filepath.Rel never reported
// ".", which is exactly what a path above Root on the same volume produces.
func TestReadAccessConfAboveRootTerminates(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(filepath.Clean(root))

	s := NewHTTPStaticServer(root, true)
	s.Upload = true

	ac := s.readAccessConf(parent)
	if !ac.Upload {
		t.Errorf("access conf above root = %+v, want the server defaults (Upload=true)", ac)
	}
	// Also exercise a path that cannot be related to Root at all.
	if ac := s.readAccessConf(""); ac.Upload != true {
		t.Errorf("empty path access conf = %+v, want defaults", ac)
	}
}

// Uploading (or renaming to) the access-control file must be impossible over
// HTTP: it would let an upload-only user grant themselves delete/edit rights,
// because the handler invalidates the config cache right after a write.
func TestAccessControlFileIsNotWritable(t *testing.T) {
	if err := checkFilename(YAMLCONF); err == nil {
		t.Error("checkFilename accepted .ghs.yml")
	}
	if err := checkPathSegment(YAMLCONF); err == nil {
		t.Error("checkPathSegment accepted .ghs.yml")
	}
	if err := checkFilename("normal.txt"); err != nil {
		t.Errorf("checkFilename rejected a normal name: %v", err)
	}
}

// Deleting the share root itself must be refused: isUnderRoot reports true for
// equality, so DELETE / used to wipe the whole share in one request.
func TestDeleteShareRootRefused(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Delete = true

	req := httptest.NewRequest("DELETE", "/", nil)
	req.URL.Path = "/"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE /: got status %d, want 400", rec.Code)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("share contents were deleted: %v", err)
	}
}

// The .ghs.yml body carries the per-user tokens that canUploadByToken /
// canEditByToken accept, so it must never be served over HTTP.
func TestAccessControlFileIsNotServed(t *testing.T) {
	root := t.TempDir()
	body := "users:\n  - name: alice\n    token: SECRET\n"
	if err := os.WriteFile(filepath.Join(root, YAMLCONF), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPStaticServer(root, true)
	s.Delete = true // used to be enough to read it

	req := httptest.NewRequest("GET", "/"+YAMLCONF, nil)
	req.URL.Path = "/" + YAMLCONF
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("GET /%s returned 200 with body %q", YAMLCONF, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SECRET") {
		t.Fatalf("response leaked the token: %q", rec.Body.String())
	}
}

// historyDirSize must populate the whole walked subtree in one pass: listing a
// directory with N subdirectories used to trigger N separate full walks.
func TestHistoryDirSizePopulatesSubtree(t *testing.T) {
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
	mk("a/f1.bin", 10)
	mk("a/b/f2.bin", 20)
	mk("a/b/c/f3.bin", 30)

	s := NewHTTPStaticServer(root, true)
	// Reset sharded cache
	dirInfoSize = shardedDirSize{}

	if got := s.historyDirSize("a"); got != 60 {
		t.Errorf("historyDirSize(a) = %d, want 60", got)
	}
	// b and b/c must already be cached from that single walk.
	if size, ok := dirInfoSize.peek("a/b"); !ok || size != 50 {
		t.Errorf("a/b not populated by the walk: size=%d ok=%v, want 50", size, ok)
	}
	if size, ok := dirInfoSize.peek("a/b/c"); !ok || size != 30 {
		t.Errorf("a/b/c not populated by the walk: size=%d ok=%v, want 30", size, ok)
	}

	// invalidatePath drops the directory and its ancestors, but not siblings.
	dirInfoSize.invalidatePath("a/b/c")
	if _, ok := dirInfoSize.peek("a/b/c"); ok {
		t.Error("invalidatePath left the entry for a/b/c")
	}
	if _, ok := dirInfoSize.peek("a"); ok {
		t.Error("invalidatePath left the ancestor entry for a")
	}
}
