package main

import (
	"net"
	"net/http"
	"os"
)

func getRealIP(req *http.Request) string {
	xip := req.Header.Get("X-Real-IP")
	if xip == "" {
		// net.SplitHostPort handles both IPv4 ("1.2.3.4:5678") and
		// IPv6 ("[::1]:8080") forms. The previous strings.Split on ":"
		// returned "[" for IPv6, which corrupted access logs.
		host, _, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			return req.RemoteAddr
		}
		xip = host
	}
	return xip
}

// getLocalIP returns the non loopback local IP of the host
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, address := range addrs {
		// check the address type and if it is not a loopback the display it
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsDir()
}
