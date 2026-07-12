package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPStaticServerSetsContentSecurityPolicy(t *testing.T) {
	root, err := os.MkdirTemp("", "gohttpserver-csp-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	server := NewHTTPStaticServer(root, true)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	server.ServeHTTP(recorder, request)

	if got := recorder.Header().Get("Content-Security-Policy"); got != contentSecurityPolicy {
		t.Fatalf("Content-Security-Policy header = %q, want %q", got, contentSecurityPolicy)
	}
}

// TestResolvePathTraversal ensures resolvePath collapses ".." segments
// and never returns a path above s.Root. This is the core path-safety
// guarantee every handler relies on.
func TestResolvePathTraversal(t *testing.T) {
	root, err := os.MkdirTemp("", "gohttpserver-traversal-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	s := &HTTPStaticServer{Root: root + "/"}

	cases := []struct {
		name    string
		input   string
		wantSub string // expected suffix after root (slash-normalised)
	}{
		{"plain", "foo/bar", "foo/bar"},
		{"dotdot collapse", "foo/../bar", "bar"},
		{"escape attempt", "../../etc/passwd", "etc/passwd"},
		{"deep escape", "a/../../../b", "b"},
		{"no leading slash", "sub/file.txt", "sub/file.txt"},
	}

	// Normalise both sides to forward slashes for portable comparison:
	// resolvePath returns ToSlash'd output, but root comes from
	// os.MkdirTemp with OS-native separators.
	rootNorm := filepath.ToSlash(root)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := filepath.ToSlash(s.resolvePath(c.input))
			if !strings.HasPrefix(got, rootNorm+"/") {
				t.Fatalf("resolvePath(%q) = %q, want under root %q", c.input, got, rootNorm)
			}
			suffix := strings.TrimPrefix(got, rootNorm+"/")
			if suffix != c.wantSub {
				t.Fatalf("resolvePath(%q) suffix = %q, want %q", c.input, suffix, c.wantSub)
			}
		})
	}
}
