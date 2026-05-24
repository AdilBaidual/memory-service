// Package api contains the HTTP router, handlers, and middleware for the memory service.
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

// NewDeleteSessionHandler handles DELETE /sessions/{session_id}.
// Removes all data associated with the session: memories (with cascading
// removal of entity_mentions and entity_relationships via FK) and turns.
func NewDeleteSessionHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		reqID := chimiddleware.GetReqID(r.Context())

		req, err := parseDeleteSessionRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			slog.Error("begin transaction", "error", err, "request_id", reqID, "session_id", req.SessionID)
			writeError(w, fmt.Errorf("begin transaction: %w", err))
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		memoriesDeleted, turnsDeleted, err := storage.DeleteSessionData(ctx, tx, req.SessionID)
		if err != nil {
			slog.Error("delete session data", "error", err, "request_id", reqID, "session_id", req.SessionID)
			writeError(w, fmt.Errorf("delete session: %w", err))
			return
		}

		if err := tx.Commit(ctx); err != nil {
			slog.Error("commit transaction", "error", err, "request_id", reqID)
			writeError(w, fmt.Errorf("commit transaction: %w", err))
			return
		}

		slog.Info("session deleted",
			"session_id", req.SessionID,
			"memories_deleted", memoriesDeleted,
			"turns_deleted", turnsDeleted)
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteSessionRequest holds the parsed inputs for DELETE /sessions/{session_id}.
type DeleteSessionRequest struct {
	SessionID string
}

func parseDeleteSessionRequest(r *http.Request) (*DeleteSessionRequest, error) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		return nil, &apiError{http.StatusBadRequest, "session_id is required"}
	}
	return &DeleteSessionRequest{SessionID: sessionID}, nil
}
