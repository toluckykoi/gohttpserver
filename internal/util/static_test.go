package util

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SecurityHeaders must cover every response, including the login page and the
// SPA assets, which are served outside the file-server package and therefore
// used to go out with no CSP / X-Frame-Options at all.
func TestSecurityHeadersAppliedToEveryResponse(t *testing.T) {
	const csp = "default-src 'self'"
	h := SecurityHeaders(csp)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/-/login", nil))

	for header, want := range map[string]string{
		"Content-Security-Policy": csp,
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	// HSTS only over TLS: sending it from a plain-HTTP server would pin
	// browsers to https on a host that cannot serve it.
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS set on a non-TLS request: %q", got)
	}
}

func staticAssetServer() http.Handler {
	body := []byte(strings.Repeat("console.log('x');\n", 200))
	return StaticAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
}

func TestStaticAssetsCacheControl(t *testing.T) {
	h := staticAssetServer()

	cases := map[string]string{
		// Content-hashed bundle files may be cached forever.
		"/-/frontend/assets/index-DT3XY-O7.js": "public, max-age=31536000, immutable",
		// Anything whose name can stay the same must revalidate.
		"/-/frontend/favicon.png": "no-cache, max-age=60",
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if got := rec.Header().Get("Cache-Control"); got != want {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, want)
		}
	}
}

func TestStaticAssetsGzip(t *testing.T) {
	h := staticAssetServer()

	req := httptest.NewRequest("GET", "/-/frontend/assets/app-abc123.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length should be dropped when compressing, got %q", got)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Errorf("Vary = %q, want it to include Accept-Encoding", got)
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not valid gzip: %v", err)
	}
	defer zr.Close()
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("console.log('x');\n", 200)
	if string(got) != want {
		t.Errorf("decompressed body mismatch: %d bytes, want %d", len(got), len(want))
	}
}

// A range request must be answered identity-encoded: byte ranges of the
// compressed stream are not ranges of the file the client asked for.
func TestStaticAssetsSkipsGzipForRangeAndHead(t *testing.T) {
	h := staticAssetServer()

	for name, req := range map[string]*http.Request{
		"range": httptest.NewRequest("GET", "/-/frontend/assets/app-abc123.js", nil),
		"head":  httptest.NewRequest("HEAD", "/-/frontend/assets/app-abc123.js", nil),
	} {
		req.Header.Set("Accept-Encoding", "gzip")
		if name == "range" {
			req.Header.Set("Range", "bytes=0-9")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("%s request was compressed (Content-Encoding=%q)", name, got)
		}
	}
}

func TestAccessTypeOf(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1":      "LOCAL",
		"::1":            "LOCAL",
		"192.168.1.20":   "LOCAL",
		"10.1.2.3":       "LOCAL",
		"100.99.39.39":   "LOCAL", // CGNAT / Tailscale
		"fe80::1":        "LOCAL",
		"203.0.113.77":   "FRP",
		"8.8.8.8":        "FRP",
		"":               "UNKNOWN",
		"not-an-ip-addr": "UNKNOWN",
	}
	for ip, want := range cases {
		if got := AccessTypeOf(ip); got != want {
			t.Errorf("AccessTypeOf(%q) = %q, want %q", ip, got, want)
		}
	}
}

func TestIsPrivateIPNil(t *testing.T) {
	if IsPrivateIP(nil) {
		t.Error("IsPrivateIP(nil) = true, want false")
	}
}
