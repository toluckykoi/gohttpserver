package util

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CSPNonceKey 是每请求 CSP nonce 在 context 中的键。
type CSPNonceKey struct{}

// CSPNonceFromContext 返回本请求的 CSP nonce；未设置时返回空串。
func CSPNonceFromContext(ctx context.Context) string {
	if v := ctx.Value(CSPNonceKey{}); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// SecurityHeaders 给每个响应加安全头，csp 中的 nonce 占位符按请求替换为随机值
// 并写入 context 供内嵌页面使用；csp 为空则不下发 CSP。
func SecurityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if csp != "" {
				nonce := cspNonce()
				h.Set("Content-Security-Policy", strings.ReplaceAll(csp, "{CSP_NONCE}", nonce))
				r = r.WithContext(context.WithValue(r.Context(), CSPNonceKey{}, nonce))
			}
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			// 仅当本进程直接提供 TLS 时下发 HSTS；若由代理终止 TLS，则由代理设置
			if r.TLS != nil {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// cspNonce 生成 128 位随机 nonce。
func cspNonce() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败几乎不可能；退化为时间戳保证非空且每请求不同。
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf[:])
}

// unsafeMethods 是需要 CSRF 防护的方法；WebDAV 动词无法由 HTML 表单发起，不列。
var unsafeMethods = map[string]bool{
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodDelete: true,
	http.MethodPatch:  true,
}

// OriginCheckMiddleware 对带 Origin 的不安全方法请求做同源校验，host 不一致即拒绝，
// 是 SameSite=Lax 之外的 CSRF 纵深防御；非浏览器客户端不带 Origin，直接放行。
func OriginCheckMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unsafeMethods[r.Method] {
			if origin := r.Header.Get("Origin"); origin != "" {
				oj, err := url.Parse(origin)
				// "null"（沙箱 iframe）或解析不出 host 的 Origin 一律拒绝。
				if err != nil || oj.Host == "" || !strings.EqualFold(oj.Host, r.Host) {
					slog.Warn("cross-origin request rejected",
						slog.String("origin", SanitizeLogInput(origin)),
						slog.String("host", SanitizeLogInput(r.Host)),
						slog.String("method", r.Method),
						slog.String("path", SanitizeLogInput(r.URL.Path)),
					)
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequestIDMiddleware 为每个请求生成唯一 ID，写入 context 与 X-Request-ID 响应头。
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := generateRequestID()
		ctx := WithRequestID(r.Context(), requestID)
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware 以结构化字段记录 HTTP 请求日志。
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := RequestIDFromContext(r.Context())
			clientIP := GetClientIP(r)
			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// 在 handler 返回后才递减，使一次长上传在其整个耗时内都计为一个在途请求
			done := metrics.requestStarted()
			defer done()

			next.ServeHTTP(rw, r)

			latency := time.Since(start).Milliseconds()
			metrics.Observe(rw.statusCode, rw.bytes)

			logger.Info("HTTP request",
				RequestIDAttr(requestID),
				ClientIPAttr(clientIP),
				// access_type 把 LOCAL/FRP 的分类合并进这一条日志
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

// responseWriter 捕获状态码与响应字节数，供日志与指标使用。
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

// Flush 必须转发 http.Flusher，否则流式 handler 的进度输出被静默缓冲到返回才发出。
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ReadFrom 保留 sendfile/零拷贝路径，退回 io.Copy 会让大文件下载多一次用户态拷贝。
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

// generateRequestID 生成 <时间戳>-<64 位随机数>，时间戳使 ID 大致可排序。
func generateRequestID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand 失败几乎不可能发生，降级为纳秒时间戳而不是丢弃请求
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return time.Now().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(buf[:])
}

// HealthCheckHandler 返回健康检查 handler，可挂载于 /healthz 或 /-/healthz。
func HealthCheckHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `"}`))
	}
}
