// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/config"
)

// NewRouter builds and returns the chi router with all middleware and routes registered.
func NewRouter(pool *pgxpool.Pool, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(MyRecoverMiddleware)
	r.Use(RequestLogger)
	r.Use(BodySizeLimiter(1 << 20)) // 1 MB

	r.Get("/health", NewHealthHandler(pool, cfg))

	return r
}
