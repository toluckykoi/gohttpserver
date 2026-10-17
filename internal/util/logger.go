package util

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// LogFormat 日志输出格式。
type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// LogLevel 日志级别。
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LoggerConfig 日志配置。
type LoggerConfig struct {
	Level  LogLevel
	Format LogFormat
}

// ParseLogLevel 解析日志级别字符串，无法识别时回退到 info。
func ParseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, nil
	}
}

// ParseLogFormat 解析日志格式字符串，无法识别时回退到 json。
func ParseLogFormat(s string) (LogFormat, error) {
	switch strings.ToLower(s) {
	case "json":
		return LogFormatJSON, nil
	case "text":
		return LogFormatText, nil
	default:
		return LogFormatJSON, nil
	}
}

// RequestIDKey 是请求 ID 在 context 中的键。
type RequestIDKey struct{}

var (
	defaultLogger *slog.Logger
	loggerInit    atomic.Bool
)

// InitLogger 初始化全局结构化日志器，只在启动时调用一次；重复调用会被忽略。
func InitLogger(opts *slog.HandlerOptions) {
	if loggerInit.Swap(true) {
		return
	}
	if opts == nil {
		opts = &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
}

// InitLoggerWithConfig 按配置初始化日志器（级别与格式）。
func InitLoggerWithConfig(cfg LoggerConfig) {
	level, _ := ParseLogLevel(string(cfg.Level))
	format, _ := ParseLogFormat(string(cfg.Format))

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}

	if format == LogFormatText {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)
	loggerInit.Store(true)
}

// Logger 返回全局结构化日志器。
func Logger() *slog.Logger {
	if defaultLogger == nil {
		InitLogger(nil)
	}
	return defaultLogger
}

// WithRequestID 把请求 ID 写入 context。
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey{}, requestID)
}

// RequestIDFromContext 从 context 取出请求 ID，无则返回空串。
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(RequestIDKey{}); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}

// RequestIDAttr 构造请求 ID 的 slog.Attr。
func RequestIDAttr(id string) slog.Attr {
	return slog.String("request_id", id)
}

// ClientIPAttr 构造客户端 IP 的 slog.Attr。
func ClientIPAttr(ip string) slog.Attr {
	return slog.String("client_ip", ip)
}

// MethodAttr 构造 HTTP 方法的 slog.Attr。
func MethodAttr(method string) slog.Attr {
	return slog.String("method", method)
}

// PathAttr 构造请求路径的 slog.Attr。
func PathAttr(path string) slog.Attr {
	return slog.String("path", path)
}

// StatusAttr 构造 HTTP 状态码的 slog.Attr。
func StatusAttr(status int) slog.Attr {
	return slog.Int("status", status)
}

// LatencyAttr 构造耗时（毫秒）的 slog.Attr。
func LatencyAttr(ms int64) slog.Attr {
	return slog.Int64("latency_ms", ms)
}

// ErrorAttr 构造错误信息的 slog.Attr。
func ErrorAttr(err error) slog.Attr {
	return slog.String("error", err.Error())
}

// PrettyJSON 返回缩进格式的 JSON 字符串，用于调试。
func PrettyJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
