// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/config"
	"memory-service/internal/extraction"
	"memory-service/internal/retrieval"
)

// NewRouter builds and returns the chi router with all middleware and routes registered.
// ext and ret may be nil when OPENAI_API_KEY is not set — handlers degrade gracefully.
func NewRouter(
	pool *pgxpool.Pool,
	cfg *config.Config,
	ext *extraction.Extractor,
	ret retrieval.Retriever,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(RecoverMiddleware)
	r.Use(RequestLogger)
	r.Use(BodySizeLimiter(1 << 20)) // 1 MB

	r.Get("/health", NewHealthHandler(pool, cfg))

	r.Post("/turns", NewTurnsHandler(pool, ext))
	r.Post("/recall", NewRecallHandler(pool, ret))
	r.Post("/search", NewSearchHandler(pool, ret))

	r.Get("/users/{user_id}/memories", NewListUserMemoriesHandler(pool))
	r.Delete("/sessions/{session_id}", NewDeleteSessionHandler(pool))
	r.Delete("/users/{user_id}", NewDeleteUserHandler(pool))

	return r
}
