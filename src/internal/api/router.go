// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/config"
	"memory-service/internal/usecase"
)

// NewRouter builds and returns the chi router with all middleware and routes registered.
func NewRouter(
	pool *pgxpool.Pool,
	cfg *config.Config,
	turns *usecase.IngestTurnUsecase,
	recall *usecase.RecallUsecase,
	search *usecase.SearchUsecase,
	memories *usecase.ListMemoriesUsecase,
	deleteSession *usecase.DeleteSessionUsecase,
	deleteUser *usecase.DeleteUserUsecase,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(RecoverMiddleware)
	r.Use(RequestLogger)
	r.Use(BodySizeLimiter(1 << 20)) // 1 MB

	r.Get("/health", NewHealthHandler(pool, cfg))

	r.Post("/turns", NewTurnsHandler(turns))
	r.Post("/recall", NewRecallHandler(recall))
	r.Post("/search", NewSearchHandler(search))

	r.Get("/users/{user_id}/memories", NewListUserMemoriesHandler(memories))
	r.Delete("/sessions/{session_id}", NewDeleteSessionHandler(deleteSession))
	r.Delete("/users/{user_id}", NewDeleteUserHandler(deleteUser))

	return r
}
