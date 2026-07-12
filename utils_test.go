package main

import (
	"net/http"
	"testing"
)

// getRealIP smoke test: ensures the helper parses the common
// RemoteAddr shapes, including IPv6 (which the previous strings.Split
// implementation corrupted into "[").
func TestGetRealIPFromRemoteAddr(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xRealIP    string
		want       string
	}{
		{"ipv4", "1.2.3.4:5678", "", "1.2.3.4"},
		{"ipv6 loopback", "[::1]:8080", "", "::1"},
		{"ipv6 link-local", "[fe80::1]:12345", "", "fe80::1"},
		{"x-real-ip override", "1.2.3.4:5678", "9.9.9.9", "9.9.9.9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", "/", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.RemoteAddr = c.remoteAddr
			if c.xRealIP != "" {
				req.Header.Set("X-Real-IP", c.xRealIP)
			}
			if got := getRealIP(req); got != c.want {
				t.Fatalf("getRealIP() = %q, want %q", got, c.want)
			}
		})
	}
}
