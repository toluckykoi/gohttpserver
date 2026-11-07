package util

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 第 8 轮审计回归：Origin 同源校验与 XFF 最右元素。

func TestRound8OriginCheck(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := OriginCheckMiddleware(next)

	cases := []struct {
		name, method, origin, host string
		want                       int
	}{
		{"same-origin POST", "POST", "http://files.example.com", "files.example.com", 200},
		{"same-origin with port", "PUT", "https://x:8443", "x:8443", 200},
		{"cross-origin POST", "POST", "http://evil.example.net", "files.example.com", 403},
		{"null origin", "POST", "null", "files.example.com", 403},
		{"no origin (curl/webdav)", "PUT", "", "files.example.com", 200},
		{"GET not checked", "GET", "http://evil.example.net", "files.example.com", 200},
		{"delete cross-origin", "DELETE", "http://evil.example.net", "files.example.com", 403},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, "/", nil)
		req.Host = tc.host
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
}

func TestRound8GetClientIPUsesRightmostXFF(t *testing.T) {
	SetTrustProxyHeaders(true)
	t.Cleanup(func() { SetTrustProxyHeaders(false) })

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.0.2.7:1234"
	// append 型反代：客户端自带的伪造值在前，真实 IP 由信任代理追加在最后。
	req.Header.Set("X-Forwarded-For", "6.6.6.6, 7.7.7.7, 198.51.100.5")
	if got := GetClientIP(req); got != "198.51.100.5" {
		t.Errorf("GetClientIP = %q, want rightmost element 198.51.100.5", got)
	}
	req.Header.Set("X-Forwarded-For", "198.51.100.5")
	if got := GetClientIP(req); got != "198.51.100.5" {
		t.Errorf("GetClientIP single element = %q, want 198.51.100.5", got)
	}
	req.Header.Del("X-Forwarded-For")
	req.Header.Set("X-Real-IP", "198.51.100.6")
	if got := GetClientIP(req); got != "198.51.100.6" {
		t.Errorf("GetClientIP X-Real-IP = %q, want 198.51.100.6", got)
	}
	// 未开启 --xheaders 时不采信任何代理头。
	SetTrustProxyHeaders(false)
	if got := GetClientIP(req); got != "192.0.2.7" {
		t.Errorf("GetClientIP without xheaders = %q, want RemoteAddr host", got)
	}
}
