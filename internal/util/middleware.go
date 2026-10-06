package util

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// SecurityHeaders applies the HTTP security headers to every response.
//
// It lives at the outermost layer (wrapped around the whole router in main)
// because the file handler used to set these itself: the login page and the
// embedded SPA assets are served by other packages and therefore went out with
// no CSP, no X-Frame-Options and no nosniff at all — on the most
// phishing-sensitive page of the app.
//
// csp is passed in rather than hard-coded so the file-server package stays the
// single source of truth for the policy that matches the frontend it embeds.
func SecurityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			// HSTS only when this process actually serves TLS; behind a
			// TLS-terminating proxy the operator sets it there instead.
			if r.TLS != nil {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMiddleware adds a unique request ID to each request context.
// The request ID is returned in the X-Request-ID response header.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := generateRequestID()
		ctx := WithRequestID(r.Context(), requestID)

		// Add to response header for client correlation
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs HTTP requests with structured fields.
// Uses the request ID from context if present.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := RequestIDFromContext(r.Context())
			clientIP := GetClientIP(r)

			// Wrap ResponseWriter to capture status code and byte count.
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Track in-flight requests for /metrics. Decremented after the
			// handler returns so a long upload counts as one in-flight
			// request for its whole duration.
			done := metrics.requestStarted()
			defer done()

			next.ServeHTTP(rw, r)

			latency := time.Since(start).Milliseconds()
			metrics.Observe(rw.statusCode, rw.bytes)

			logger.Info("HTTP request",
				RequestIDAttr(requestID),
				ClientIPAttr(clientIP),
				// LOCAL / FRP classification used to be produced by a second
				// logger wrapping the file handler, which meant every request
				// was logged twice (and reparsed the private-IP CIDRs each
				// time). One line now carries both.
				slog.String("access_type", AccessTypeOf(clientIP)),
				MethodAttr(r.Method),
				PathAttr(r.URL.Path),
				StatusAttr(rw.statusCode),
				LatencyAttr(latency),
				slog.Int("bytes", rw.bytes),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture the status code
// and the number of body bytes written for logging and metrics.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(p []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(p)
	rw.bytes += n
	return n, err
}

// Flush forwards to the underlying writer when it supports flushing.
// Without this the wrapper would hide http.Flusher from handlers that
// stream (the upload progress NDJSON path asserts for it), silently
// buffering progress output until the handler returns.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ReadFrom preserves the sendfile/zero-copy fast path that net/http's
// writer exposes for large downloads. Falling back to a plain io.Copy
// would add a userspace copy for every file served.
func (rw *responseWriter) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := rw.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(src)
		rw.bytes += int(n)
		return n, err
	}
	n, err := io.Copy(rw.ResponseWriter, src)
	rw.bytes += int(n)
	return n, err
}

// generateRequestID builds an ID of the form
// <UTC-ish timestamp>-<random hex>. The timestamp makes IDs roughly
// sortable by arrival; the 64 random bits make collisions negligible.
//
// The previous implementation fabricated the random part from the low
// bits of time.Now().UnixNano() and called time.Sleep(1) between
// characters. That was both weakly random and, on platforms whose sleep
// granularity is ~1ms, cost several milliseconds per request.
func generateRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand failing is effectively impossible on supported
		// platforms; degrade to a nanosecond value rather than dropping
		// the request.
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return time.Now().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(buf[:])
}

// HealthCheckHandler returns a simple health check handler.
// Can be mounted at /healthz or /-/healthz.
func HealthCheckHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
	}
}
