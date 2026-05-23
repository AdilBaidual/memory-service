// Package main is the entrypoint for the memory-service HTTP server.
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

	"memory-service/internal/api"
	"memory-service/internal/config"
	"memory-service/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	level := parseLogLevel(cfg.LogLevel)
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	slog.Info("config loaded", "port", cfg.Port, "log_level", cfg.LogLevel)

	if cfg.OpenAIAPIKey == "" {
		slog.Warn("OPENAI_API_KEY not set; LLM-dependent endpoints will return errors when called")
	}

	if cfg.CohereAPIKey != "" {
		slog.Info("reranker configured", "provider", "cohere", "model", cfg.CohereRerankModel)
	} else {
		slog.Warn("reranker not configured",
			"note", "set COHERE_API_KEY to enable cross-encoder reranking; /recall will use RRF-only ranking until configured")
	}

	ctx := context.Background()

	bootPool, err := storage.NewBootstrapPool(ctx, cfg)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	slog.Info("db pool connected")

	count, err := storage.ApplyMigrations(ctx, bootPool)
	if err != nil {
		bootPool.Close()
		slog.Error("failed to apply migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations applied", "count", count)
	bootPool.Close()

	pool, err := storage.NewPool(ctx, cfg)
	if err != nil {
		slog.Error("failed to create pool with pgvector", "error", err)
		os.Exit(1)
	}

	router := api.NewRouter(pool, cfg)

	addr := fmt.Sprintf(":%s", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 65 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	slog.Info("shutdown signal received", "signal", sigCtx.Err())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
	slog.Info("server stopped")

	pool.Close()
	slog.Info("shutdown complete")
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
