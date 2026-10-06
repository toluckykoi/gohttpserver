package util

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// SanitizeLogInput removes control characters (except space, tab, newline)
// from user-controlled input before logging. Prevents log injection attacks
// where attackers craft filenames/URLs containing newlines to inject fake
// log entries.
func SanitizeLogInput(s string) string {
	// Remove control characters except \t \n \r (space is 0x20, printable)
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1 // remove the character
		}
		return r
	}, s)
}

// GetLocalIP returns the non loopback local IP of the host
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	// Prefer a private LAN address over anything else: the first non-loopback
	// IPv4 on a typical developer/host machine is often a Tailscale/CGNAT
	// (100.64.0.0/10) or VPN address, which is useless as the "share this URL
	// with your colleagues" address the admin panel shows.
	fallback := ""
	for _, address := range addrs {
		ipnet, ok := address.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
			continue
		}
		if isLANAddress(ipnet.IP) {
			return ipnet.IP.String()
		}
		if fallback == "" {
			fallback = ipnet.IP.String()
		}
	}
	return fallback
}

// isLANAddress reports whether ip is in one of the LAN/CGNAT ranges a browser
// on the same network could actually reach.
func isLANAddress(ip net.IP) bool {
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10"} {
		if _, network, err := net.ParseCIDR(cidr); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// privateNets are the ranges treated as internal. Parsed once at init: the
// access-log path used to rebuild and reparse this list for EVERY request.
// 100.64.0.0/10 (CGNAT/Tailscale) and fe80::/10 (link-local) are included so a
// VPN/tailnet client is not mislabelled as a public "FRP" visitor.
var privateNets = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"100.64.0.0/10",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

// IsPrivateIP reports whether ip falls in a private/loopback/link-local range.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range privateNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// AccessTypeOf classifies a client address for the access log: "LOCAL" for
// private/loopback clients, "FRP" for anything arriving from a public address
// (i.e. through a tunnel or port-forward). An unparseable or empty address is
// reported as UNKNOWN rather than being silently called public.
func AccessTypeOf(ipStr string) string {
	if ipStr == "" {
		return "UNKNOWN"
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "UNKNOWN"
	}
	if IsPrivateIP(ip) {
		return "LOCAL"
	}
	return "FRP"
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsDir()
}

// ─── Proxy header trust control ─────────────────────────────────────────────
//
// SECURITY FIX: Previously GetClientIP unconditionally trusted X-Forwarded-For
// and X-Real-IP headers. When the server is directly exposed to the network
// (not behind a reverse proxy), any client can forge these headers to:
//   - Bypass login brute-force rate limiting (each fake IP gets its own
//     counter, so the attacker never gets locked out).
//   - Inject fake IPs into audit logs, obscuring attack traces.
//
// Now proxy headers are only honoured when the operator has explicitly
// enabled --xheaders (indicating they are behind a trusted proxy). main.go
// calls SetTrustProxyHeaders(true) when --xheaders is set.

var (
	trustProxyHeadersMu sync.RWMutex
	trustProxyHeaders   bool
)

// SetTrustProxyHeaders enables or disables trust of X-Forwarded-For /
// X-Real-IP headers. Should be called once at startup from main.go when
// --xheaders is parsed.
func SetTrustProxyHeaders(v bool) {
	trustProxyHeadersMu.Lock()
	defer trustProxyHeadersMu.Unlock()
	trustProxyHeaders = v
}

// isTrustProxyHeaders returns whether proxy headers should be trusted.
func isTrustProxyHeaders() bool {
	trustProxyHeadersMu.RLock()
	defer trustProxyHeadersMu.RUnlock()
	return trustProxyHeaders
}

// GetClientIP extracts the client IP address from the request.
//
// When --xheaders is enabled (server behind a trusted reverse proxy),
// it checks X-Forwarded-For and X-Real-IP headers first, then falls
// back to RemoteAddr.
//
// When --xheaders is NOT enabled (server directly exposed), it uses
// only RemoteAddr, preventing IP spoofing via forged proxy headers.
func GetClientIP(r *http.Request) string {
	if isTrustProxyHeaders() {
		// Check X-Forwarded-For header (common for reverse proxies)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For can contain multiple IPs: client, proxy1, proxy2
			// Take the first one (the original client)
			if idx := strings.Index(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}

		// Check X-Real-IP header (nginx, etc.)
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // RemoteAddr might not have port
	}
	return ip
}
