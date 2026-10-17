package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateRequestIDIsUnique(t *testing.T) {
	const n = 2000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := generateRequestID()
		if id == "" {
			t.Fatal("generateRequestID returned empty string")
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate request id: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestRequestIDMiddlewareSetsHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	var seenInContext string
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInContext = RequestIDFromContext(r.Context())
	}))
	h.ServeHTTP(rec, req)

	header := rec.Header().Get("X-Request-ID")
	if header == "" {
		t.Fatal("X-Request-ID response header missing")
	}
	if seenInContext != header {
		t.Fatalf("context id %q != header id %q", seenInContext, header)
	}
}

// response wrapper 必须保持实现 http.Flusher，避免流式 handler（上传进度）被静默缓冲。
func TestResponseWriterImplementsFlusher(t *testing.T) {
	var _ http.Flusher = (*responseWriter)(nil)
}
