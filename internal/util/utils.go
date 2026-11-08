package util

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
)

// SanitizeLogInput 记录日志前剥离用户可控输入中的控制字符，防止构造文件名注入日志。
func SanitizeLogInput(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
}

// GetLocalIP 返回可用于分享的私网地址（仅 10/8 与 192.168/16），找不到则返回 ""。
// CGNAT、link-local、VPN 等网段对分享用途不可达。
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		ipnet, ok := address.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
			continue
		}
		if IsLANAddress(ipnet.IP) {
			return ipnet.IP.String()
		}
	}
	return ""
}

// IsLANAddress 判断 ip 是否属于管理面板视为可分享的局域网段（仅 10/8 与 192.168/16）。
func IsLANAddress(ip net.IP) bool {
	for _, cidr := range []string{"10.0.0.0/8", "192.168.0.0/16"} {
		if _, network, err := net.ParseCIDR(cidr); err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// privateNets 内网网段列表，init 阶段解析一次。
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

// IsPrivateIP 判断 ip 是否属于私网/loopback/link-local 网段。
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

// AccessTypeOf 为访问日志分类客户端地址：私网/loopback 为 "LOCAL"，公网地址
// （隧道或端口转发）为 "FRP"；无法解析的地址返回 "UNKNOWN" 而非默认为公网。
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

var (
	trustProxyHeadersMu sync.RWMutex
	trustProxyHeaders   bool
)

// SetTrustProxyHeaders 开关 X-Forwarded-For / X-Real-IP 的信任，由 main.go 在解析 --xheaders 后调用。
func SetTrustProxyHeaders(v bool) {
	trustProxyHeadersMu.Lock()
	defer trustProxyHeadersMu.Unlock()
	trustProxyHeaders = v
}

// isTrustProxyHeaders 返回是否信任代理头。
func isTrustProxyHeaders() bool {
	trustProxyHeadersMu.RLock()
	defer trustProxyHeadersMu.RUnlock()
	return trustProxyHeaders
}

// GetClientIP 提取客户端 IP：仅在 --xheaders 开启时采信 X-Forwarded-For / X-Real-IP，否则只用 RemoteAddr。
// SECURITY: XFF 取最右元素——append 型反代把真实 IP 追加在末尾，取最左可被客户端伪造 IP 绕过限流。
func GetClientIP(r *http.Request) string {
	if isTrustProxyHeaders() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// XFF 形如 "client, proxy1, proxy2"，最右元素由最近的信任代理写入。
			if idx := strings.LastIndex(xff, ","); idx != -1 {
				return strings.TrimSpace(xff[idx+1:])
			}
			return strings.TrimSpace(xff)
		}

		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // RemoteAddr 可能不带端口
	}
	return ip
}
