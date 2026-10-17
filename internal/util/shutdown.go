package util

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log/slog"
)

// ShutdownManager 负责服务的优雅退出。
type ShutdownManager struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	cleanupFuncs    []func() error
}

// NewShutdownManager 创建退出管理器；logger 为 nil 时用 slog.Default()，timeout 为 0 时用 30s。
func NewShutdownManager(server *http.Server, logger *slog.Logger, timeout time.Duration) *ShutdownManager {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &ShutdownManager{
		server:          server,
		logger:          logger,
		shutdownTimeout: timeout,
	}
}

// RegisterCleanup 注册退出时的清理函数，执行顺序为 LIFO。
func (sm *ShutdownManager) RegisterCleanup(fn func() error) {
	sm.cleanupFuncs = append(sm.cleanupFuncs, fn)
}

// WaitForShutdown 阻塞等待退出信号，随后优雅关闭服务并执行清理函数。
func (sm *ShutdownManager) WaitForShutdown() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	sm.logger.Info("shutdown signal received, starting graceful shutdown")
	return sm.Shutdown()
}

// Shutdown 排空 HTTP 服务后按注册逆序（LIFO）执行清理；即使排空失败清理也会执行。
func (sm *ShutdownManager) Shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), sm.shutdownTimeout)
	defer cancel()

	if err := sm.server.Shutdown(shutdownCtx); err != nil {
		sm.logger.Error("server shutdown failed", ErrorAttr(err))
		if closeErr := sm.server.Close(); closeErr != nil {
			sm.logger.Error("server force close failed", ErrorAttr(closeErr))
		}
		sm.runCleanups()
		return err
	}

	sm.runCleanups()
	sm.logger.Info("graceful shutdown completed")
	return nil
}

// runCleanups 逆序（最新注册者优先）执行清理函数，单个失败不影响其余执行。
func (sm *ShutdownManager) runCleanups() {
	for i := len(sm.cleanupFuncs) - 1; i >= 0; i-- {
		if err := sm.cleanupFuncs[i](); err != nil {
			sm.logger.Error("cleanup failed", ErrorAttr(err), slog.Int("cleanup_index", i))
		}
	}
}
