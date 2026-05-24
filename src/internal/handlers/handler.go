// Package handlers contains the HTTP layer for the memory service.
package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/usecase"
)

type TurnIngester interface {
	Ingest(ctx context.Context, in usecase.TurnInput) (usecase.TurnOutput, error)
}

type Recaller interface {
	Recall(ctx context.Context, in usecase.RecallInput) usecase.RecallOutput
}

type Searcher interface {
	Search(ctx context.Context, in usecase.SearchInput) usecase.SearchOutput
}

type MemoryLister interface {
	List(ctx context.Context, in usecase.MemoriesInput) (usecase.MemoriesOutput, error)
}

type SessionDeleter interface {
	Delete(ctx context.Context, sessionID string) (usecase.DeleteSessionResult, error)
}

type UserDeleter interface {
	Delete(ctx context.Context, userID string) error
}

type Handler struct {
	log      *slog.Logger
	pool     *pgxpool.Pool // health check only
	turns    TurnIngester
	recall   Recaller
	search   Searcher
	memories MemoryLister
	session  SessionDeleter
	user     UserDeleter
}

func NewHandler(
	pool *pgxpool.Pool,
	turns TurnIngester,
	recall Recaller,
	search Searcher,
	memories MemoryLister,
	session SessionDeleter,
	user UserDeleter,
) *Handler {
	return &Handler{
		log:      slog.Default(),
		pool:     pool,
		turns:    turns,
		recall:   recall,
		search:   search,
		memories: memories,
		session:  session,
		user:     user,
	}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(RecoverMiddleware)
	r.Use(RequestLogger)
	r.Use(BodySizeLimiter(1 << 20)) // 1 MB

	r.Get("/health", h.handleHealth)

	r.Post("/turns", h.handleTurns)
	r.Post("/recall", h.handleRecall)
	r.Post("/search", h.handleSearch)

	r.Get("/users/{user_id}/memories", h.handleListMemories)
	r.Delete("/sessions/{session_id}", h.handleDeleteSession)
	r.Delete("/users/{user_id}", h.handleDeleteUser)

	return r
}
