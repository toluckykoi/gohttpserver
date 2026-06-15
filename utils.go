package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// func formatSize(file os.FileInfo) string {
// 	if file.IsDir() {
// 		return "-"
// 	}
// 	size := file.Size()
// 	switch {
// 	case size > 1024*1024:
// 		return fmt.Sprintf("%.1f MB", float64(size)/1024/1024)
// 	case size > 1024:
// 		return fmt.Sprintf("%.1f KB", float64(size)/1024)
// 	default:
// 		return strconv.Itoa(int(size)) + " B"
// 	}
// 	return ""
// }

func getRealIP(req *http.Request) string {
	xip := req.Header.Get("X-Real-IP")
	if xip == "" {
		xip = strings.Split(req.RemoteAddr, ":")[0]
	}
	return xip
}

func SublimeContains(s, substr string) bool {
	rs, rsubstr := []rune(s), []rune(substr)
	if len(rsubstr) > len(rs) {
		return false
	}

	var ok = true
	var i, j = 0, 0
	for ; i < len(rsubstr); i++ {
		found := -1
		for ; j < len(rs); j++ {
			if rsubstr[i] == rs[j] {
				found = j
				break
			}
		}
		if found == -1 {
			ok = false
			break
		}
		j += 1
	}
	return ok
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

// Convert path to normal paths
func cleanPath(path string) string {
	// 处理特殊情况：Windows 上 // 开头的路径
	if strings.HasPrefix(path, "//") || strings.HasPrefix(path, "\\\\") {
		// 对于 UNC 路径，先转换为单个 / 开头
		path = "/" + strings.TrimLeft(path, "/\\")
	}
	cleaned := filepath.Clean(path)
	// 如果在 Windows 上，确保使用 / 作为分隔符
	cleaned = filepath.ToSlash(cleaned)
	// 再次处理，确保多个 / 开头的路径被正确处理
	if strings.HasPrefix(cleaned, "//") {
		cleaned = "/" + strings.TrimLeft(cleaned, "/")
	}
	return cleaned
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsDir()
}
