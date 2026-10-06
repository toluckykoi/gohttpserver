package util

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
)

// LogFormat 日志输出格式
type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// LogLevel 日志级别
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LoggerConfig 日志配置
type LoggerConfig struct {
	Level  LogLevel
	Format LogFormat
}

// ParseLogLevel 解析日志级别字符串
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

// ParseLogFormat 解析日志格式字符串
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

// RequestIDKey is the context key for request ID
type RequestIDKey struct{}

var (
	// defaultLogger is the global structured logger
	defaultLogger *slog.Logger
	loggerInit    atomic.Bool
)

// InitLogger initializes the global structured logger.
// Call once at startup. If not called, a default JSON logger to stdout is used.
func InitLogger(opts *slog.HandlerOptions) {
	if loggerInit.Swap(true) {
		return // already initialized
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

// InitLoggerWithConfig 使用配置初始化日志器
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

// Logger returns the default structured logger.
func Logger() *slog.Logger {
	if defaultLogger == nil {
		InitLogger(nil)
	}
	return defaultLogger
}

// WithRequestID adds a request ID to the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey{}, requestID)
}

// RequestIDFromContext extracts the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(RequestIDKey{}); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return ""
}

// RequestIDAttr creates a slog.Attr for request ID.
func RequestIDAttr(id string) slog.Attr {
	return slog.String("request_id", id)
}

// ClientIPAttr creates a slog.Attr for client IP.
func ClientIPAttr(ip string) slog.Attr {
	return slog.String("client_ip", ip)
}

// MethodAttr creates a slog.Attr for HTTP method.
func MethodAttr(method string) slog.Attr {
	return slog.String("method", method)
}

// PathAttr creates a slog.Attr for request path.
func PathAttr(path string) slog.Attr {
	return slog.String("path", path)
}

// StatusAttr creates a slog.Attr for HTTP status.
func StatusAttr(status int) slog.Attr {
	return slog.Int("status", status)
}

// LatencyAttr creates a slog.Attr for latency in milliseconds.
func LatencyAttr(ms int64) slog.Attr {
	return slog.Int64("latency_ms", ms)
}

// ErrorAttr creates a slog.Attr for error.
func ErrorAttr(err error) slog.Attr {
	return slog.String("error", err.Error())
}

// PrettyJSON 返回格式化的 JSON 字符串，用于调试
func PrettyJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
