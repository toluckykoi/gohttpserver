package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// TestRestrictToLocalhost 覆盖 loopback 放行、远端拒绝、公开开关与畸形地址拒绝。
func TestRestrictToLocalhost(t *testing.T) {
	handler := RestrictToLocalhost(false, okHandler())
	publicHandler := RestrictToLocalhost(true, okHandler())

	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		handler    http.Handler
		wantCode   int
	}{
		{"loopback v4 allowed", "127.0.0.1:5555", "", handler, http.StatusOK},
		{"loopback v6 allowed", "[::1]:5555", "", handler, http.StatusOK},
		{"remote v4 denied", "203.0.113.7:5555", "", handler, http.StatusForbidden},
		{"remote v6 denied", "[2001:db8::1]:5555", "", handler, http.StatusForbidden},
		{"private v4 denied", "192.168.1.10:5555", "", handler, http.StatusForbidden},
		{"missing addr denied", "", "", handler, http.StatusForbidden},
		{"garbage addr denied", "not-an-ip", "", handler, http.StatusForbidden},
		{"public flag bypasses", "203.0.113.7:5555", "", publicHandler, http.StatusOK},
		// xheaders 场景：反代在 loopback，真实客户端 IP 取自 XFF 最右元素。
		{"xff remote denied", "127.0.0.1:5555", "203.0.113.9", handler, http.StatusForbidden},
		{"xff loopback allowed", "10.0.0.2:5555", "10.0.0.9, 127.0.0.1", handler, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetTrustProxyHeaders(tc.xff != "")
			defer SetTrustProxyHeaders(false)

			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
		})
	}
}

// TestRestrictToLocalhostNoSideEffects 确认被拒绝时不会调用下游 handler。
func TestRestrictToLocalhostNoSideEffects(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	handler := RestrictToLocalhost(false, next)

	req := httptest.NewRequest(http.MethodGet, "/-/sysinfo", nil)
	req.RemoteAddr = "203.0.113.7:5555"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Fatal("downstream handler must not be invoked for remote clients")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
