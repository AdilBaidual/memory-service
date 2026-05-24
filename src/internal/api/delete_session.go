package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"memory-service/internal/usecase"
)

// NewDeleteSessionHandler handles DELETE /sessions/{session_id}.
func NewDeleteSessionHandler(uc *usecase.DeleteSessionUsecase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		reqID := chimiddleware.GetReqID(r.Context())

		req, err := parseDeleteSessionRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		result, err := uc.Delete(ctx, req.SessionID)
		if err != nil {
			slog.Error("delete session", "error", err, "request_id", reqID, "session_id", req.SessionID)
			writeError(w, err)
			return
		}

		slog.Info("session deleted",
			"session_id", req.SessionID,
			"memories_deleted", result.MemoriesDeleted,
			"turns_deleted", result.TurnsDeleted)
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
