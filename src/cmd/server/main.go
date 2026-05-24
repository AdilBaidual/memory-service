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
	"memory-service/internal/extraction"
	"memory-service/internal/llm"
	"memory-service/internal/retrieval"
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

	// LLM client — nil when OPENAI_API_KEY is not set (graceful degradation).
	llmClient := llm.NewClient(cfg)
	if llmClient == nil {
		slog.Warn("openai client not configured",
			"note", "POST /turns will save turns without extraction; /recall will return empty context")
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

	// Extractor wraps the LLM client for memory extraction.
	ext := extraction.New(llmClient)

	// Retriever — Stage 4: hybrid semantic + FTS fused via RRF.
	ret := retrieval.NewHybridRetriever(pool, llmClient)

	handler := api.NewRouter(pool, cfg, ext, ret)

	addr := fmt.Sprintf(":%s", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case <-sigCtx.Done():
		slog.Info("shutdown signal received")
	case err := <-serverErr:
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

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
