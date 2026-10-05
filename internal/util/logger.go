package util

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
)

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
