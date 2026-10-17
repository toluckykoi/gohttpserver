package util

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// SecurityHeaders 给每个响应加上安全响应头；csp 由文件服务包提供，是匹配内嵌前端的唯一来源。
func SecurityHeaders(csp string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
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

// Flush 转发到底层 writer（若支持）；缺少它会让流式 handler（上传进度 NDJSON）
// 看不到 http.Flusher，进度输出被静默缓冲到 handler 返回才发出。
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ReadFrom 保留 net/http writer 为大文件下载提供的 sendfile/零拷贝路径，
// 退回 io.Copy 会让每个文件多一次用户态拷贝。
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

// generateRequestID 生成 <时间戳>-<64 位随机数>：时间戳让 ID 大致可排序，
// 随机位让碰撞概率可忽略。
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
