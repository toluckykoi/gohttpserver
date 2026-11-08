package util

import (
	"compress/gzip"
	"net/http"
	"strings"
)

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

// StaticAssets 为内嵌前端加 gzip（仅 GET、无 Range 且客户端接受）与 Cache-Control：
// 带内容哈希的 /assets/ 缓存一年，SPA 外壳与 favicon 名字不变而内容可能变，需重新验证。
func StaticAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHashedAsset(r.URL.Path) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// 内嵌文件没有验证器可用，纯 "no-cache" 会导致每次导航都全量重新下载
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

// isHashedAsset 判断路径是否指向 Vite 的内容哈希产物（/-/frontend/assets/<name>-<hash>.<ext>）。
func isHashedAsset(p string) bool {
	return strings.Contains(p, "/assets/")
}

// acceptsGzip 简单检查 Accept-Encoding：列出 gzip 的客户端唯一现实中的拒绝方式是 "gzip;q=0"。
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "gzip") && !strings.Contains(part, "q=0") {
			return true
		}
	}
	return false
}

// gzipResponseWriter 压缩响应体，并去掉 http.ServeContent 可能已写入的框架头
// （压缩后 Content-Length / Accept-Ranges 不再成立）。
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

// Flush 先刷 gzip writer 再刷底层，保证流式响应及时送达。
func (w *gzipResponseWriter) Flush() {
	_ = w.gz.Flush()
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
