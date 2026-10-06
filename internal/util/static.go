package util

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// compressibleExt lists the embedded-SPA asset types worth gzipping. The
// bundle is ~1.4 MiB of JS/CSS; serving it identity-encoded meant every cold
// load paid for all of it, even though gzip cuts it by roughly 3x.
var compressibleExt = []string{".js", ".mjs", ".css", ".html", ".svg", ".json", ".map", ".txt", ".ico"}

func compressiblePath(p string) bool {
	lower := strings.ToLower(p)
	for _, ext := range compressibleExt {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// StaticAssets wraps the embedded frontend file server with the two response
// policies a Vite bundle needs:
//
//   - gzip for text assets (only when the client asked for it, never for HEAD
//     or Range requests — a range of a gzipped body is not a range of the
//     original bytes);
//   - Cache-Control. Vite names every bundled file with a content hash
//     (element-plus-BBkW7pD9.js), so those are safe to mark immutable for a
//     year. Everything else under /-/frontend/ (the SPA shell, favicon) must
//     revalidate, because it can change without its name changing.
//
// The file server itself sets neither header: http.FileServer over an
// embed.FS has no ModTime, so it cannot even emit Last-Modified, and clients
// therefore had no way to cache or revalidate the bundle at all.
func StaticAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHashedAsset(r.URL.Path) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// No validators exist for embedded files, so "no-cache" would force
			// a full re-download on every navigation. A short max-age keeps the
			// SPA shell fresh enough without hammering the server.
			w.Header().Set("Cache-Control", "no-cache, max-age=60")
		}

		if r.Method != http.MethodGet || r.Header.Get("Range") != "" ||
			!acceptsGzip(r.Header.Get("Accept-Encoding")) || !compressiblePath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer gz.Close()

		gw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		next.ServeHTTP(gw, r)
	})
}

// isHashedAsset reports whether the path points at a Vite content-hashed
// bundle file (/-/frontend/assets/<name>-<hash>.<ext>).
func isHashedAsset(p string) bool {
	return strings.Contains(p, "/assets/")
}

// acceptsGzip does a deliberately simple Accept-Encoding check: we only need
// to know whether gzip is acceptable, and "gzip;q=0" is the only realistic way
// a client that lists gzip would still refuse it.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "gzip") && !strings.Contains(part, "q=0") {
			return true
		}
	}
	return false
}

// gzipResponseWriter deflates the body and fixes up the framing headers that
// http.ServeContent already committed to (Content-Length / Accept-Ranges are
// no longer true once the body is compressed).
type gzipResponseWriter struct {
	http.ResponseWriter
	gz        *gzip.Writer
	headerSet bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if !w.headerSet {
		w.headerSet = true
		h := w.Header()
		h.Del("Content-Length")
		h.Del("Accept-Ranges")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.headerSet {
		w.WriteHeader(http.StatusOK)
	}
	return w.gz.Write(b)
}

// Flush forwards through the gzip writer so streamed responses still reach the
// client promptly.
func (w *gzipResponseWriter) Flush() {
	_ = w.gz.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
