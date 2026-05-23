// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/config"
)

// NewRouter builds and returns the chi router with all middleware and routes registered.
func NewRouter(pool *pgxpool.Pool, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(RecoverMiddleware)
	r.Use(RequestLogger)
	r.Use(BodySizeLimiter(1 << 20)) // 1 MB

	r.Get("/health", NewHealthHandler(pool, cfg))

	r.Post("/turns", NewTurnsHandler(pool))
	r.Post("/recall", NewRecallHandler(pool))
	r.Post("/search", NewSearchHandler(pool))

	r.Get("/users/{user_id}/memories", NewListUserMemoriesHandler(pool))
	r.Delete("/sessions/{session_id}", NewDeleteSessionHandler(pool))
	r.Delete("/users/{user_id}", NewDeleteUserHandler(pool))

	return r
}
