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

// ShutdownManager handles graceful shutdown of the server.
type ShutdownManager struct {
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	cleanupFuncs    []func() error
}

// NewShutdownManager creates a new shutdown manager.
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

// RegisterCleanup adds a cleanup function to be called on shutdown.
// Cleanup functions are called in reverse order of registration (LIFO).
func (sm *ShutdownManager) RegisterCleanup(fn func() error) {
	sm.cleanupFuncs = append(sm.cleanupFuncs, fn)
}

// WaitForShutdown blocks until a shutdown signal is received,
// then gracefully shuts down the server and runs cleanup functions.
func (sm *ShutdownManager) WaitForShutdown() error {
	// Create context that listens for interrupt signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Wait for shutdown signal
	<-ctx.Done()
	sm.logger.Info("shutdown signal received, starting graceful shutdown")
	return sm.Shutdown()
}

// Shutdown gracefully drains the HTTP server and then runs every
// registered cleanup function in reverse registration order (LIFO), so
// resources that were opened first are torn down last. It is safe to
// call directly when the caller owns the signal handling; cleanup runs
// even if the HTTP drain itself fails.
func (sm *ShutdownManager) Shutdown() error {
	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), sm.shutdownTimeout)
	defer cancel()

	// Shutdown HTTP server
	if err := sm.server.Shutdown(shutdownCtx); err != nil {
		sm.logger.Error("server shutdown failed", ErrorAttr(err))
		// Force close if graceful shutdown fails
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

// runCleanups executes registered cleanup functions newest-first.
func (sm *ShutdownManager) runCleanups() {
	for i := len(sm.cleanupFuncs) - 1; i >= 0; i-- {
		if err := sm.cleanupFuncs[i](); err != nil {
			sm.logger.Error("cleanup failed", ErrorAttr(err), slog.Int("cleanup_index", i))
		}
	}
}
