package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/storage"
)

// NewDeleteUserHandler handles DELETE /users/{user_id}.
// Removes all data for the user across all tables.
func NewDeleteUserHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		reqID := chimiddleware.GetReqID(r.Context())

		req, err := parseDeleteUserRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			slog.Error("begin transaction", "error", err, "request_id", reqID, "user_id", req.UserID)
			writeError(w, fmt.Errorf("begin transaction: %w", err))
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		if err := storage.DeleteAllUserData(ctx, tx, req.UserID); err != nil {
			slog.Error("delete user data", "error", err, "request_id", reqID, "user_id", req.UserID)
			writeError(w, fmt.Errorf("delete user: %w", err))
			return
		}

		if err := tx.Commit(ctx); err != nil {
			slog.Error("commit transaction", "error", err, "request_id", reqID)
			writeError(w, fmt.Errorf("commit transaction: %w", err))
			return
		}

		slog.Info("user deleted", "user_id", req.UserID)
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteUserRequest holds the parsed inputs for DELETE /users/{user_id}.
type DeleteUserRequest struct {
	UserID string
}

func parseDeleteUserRequest(r *http.Request) (*DeleteUserRequest, error) {
	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		return nil, &apiError{http.StatusBadRequest, "user_id is required"}
	}
	return &DeleteUserRequest{UserID: userID}, nil
}
