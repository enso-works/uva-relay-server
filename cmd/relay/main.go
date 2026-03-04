package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anthropics/uvame-relay/internal/config"
	"github.com/anthropics/uvame-relay/internal/server"
)

var version = "dev"

func main() {
	cfg := config.Load()

	opts := server.Options{
		Port:         cfg.Port,
		DataDir:      cfg.DataDir,
		PingInterval: cfg.PingInterval,
		PongTimeout:  cfg.PongTimeout,
		ReclaimDays:  cfg.ReclaimDays,
		LogLevel:     cfg.LogLevel,
	}

	srv, err := server.New(opts)
	if err != nil {
		slog.Error("failed to create server", "err", err)
		os.Exit(1)
	}

	// Write port file if configured
	if cfg.PortFile != "" {
		if err := os.WriteFile(cfg.PortFile, []byte(fmt.Sprintf("%d", srv.Port())), 0o644); err != nil {
			slog.Error("failed to write port file", "err", err)
		}
	}

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	slog.Info("relay server started", "version", version, "port", srv.Port())

	<-ctx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}

	slog.Info("relay server stopped")
}
