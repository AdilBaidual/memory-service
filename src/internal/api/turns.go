// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/storage"
)

// NewTurnsHandler handles POST /turns.
func NewTurnsHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 50*time.Second)
		defer cancel()
		reqID := chimiddleware.GetReqID(r.Context())

		req, err := parseTurnRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		sanitizeTurnMessages(req.Messages)

		messagesJSON, err := json.Marshal(req.Messages)
		if err != nil {
			slog.Error("marshal messages", "error", err, "request_id", reqID)
			writeError(w, fmt.Errorf("marshal messages: %w", err))
			return
		}

		metadata := req.Metadata
		if len(metadata) == 0 {
			metadata = json.RawMessage("{}")
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			slog.Error("begin transaction", "error", err, "request_id", reqID)
			writeError(w, fmt.Errorf("begin transaction: %w", err))
			return
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		turnID, err := storage.InsertTurn(ctx, tx, storage.InsertTurnParams{
			SessionID: req.SessionID,
			UserID:    req.UserID,
			Messages:  messagesJSON,
			Timestamp: req.Timestamp,
			Metadata:  metadata,
		})
		if err != nil {
			slog.Error("insert turn", "error", err, "request_id", reqID, "session_id", req.SessionID)
			writeError(w, fmt.Errorf("insert turn: %w", err))
			return
		}

		if err := tx.Commit(ctx); err != nil {
			slog.Error("commit transaction", "error", err, "request_id", reqID)
			writeError(w, fmt.Errorf("commit transaction: %w", err))
			return
		}

		userID := ""
		if req.UserID != nil {
			userID = *req.UserID
		}
		slog.Info("turn inserted",
			"turn_id", turnID,
			"session_id", req.SessionID,
			"user_id", userID,
			"message_count", len(req.Messages),
		)

		writeJSON(w, http.StatusCreated, buildTurnResponse(turnID))
	}
}

func parseTurnRequest(r *http.Request) (*TurnRequest, error) {
	var req TurnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, &apiError{http.StatusRequestEntityTooLarge, "request body too large"}
		}
		return nil, &apiError{http.StatusBadRequest, "invalid request body: " + err.Error()}
	}
	if req.SessionID == "" {
		return nil, &apiError{http.StatusBadRequest, "session_id is required"}
	}
	if len(req.Messages) == 0 {
		return nil, &apiError{http.StatusBadRequest, "messages must be non-empty"}
	}
	if req.Timestamp.IsZero() {
		return nil, &apiError{http.StatusBadRequest, "timestamp is required"}
	}
	validRoles := map[string]bool{"user": true, "assistant": true, "tool": true}
	for i, msg := range req.Messages {
		if !validRoles[msg.Role] {
			return nil, &apiError{http.StatusBadRequest, fmt.Sprintf("messages[%d]: invalid role %q", i, msg.Role)}
		}
		if msg.Content == "" {
			return nil, &apiError{http.StatusBadRequest, fmt.Sprintf("messages[%d]: content is required", i)}
		}
	}
	return &req, nil
}

func buildTurnResponse(id string) TurnResponse {
	return TurnResponse{ID: id}
}

// TODO: подумать как улучшить
func sanitizeTurnMessages(msgs []Message) {
	for i := range msgs {
		msgs[i].Content = sanitizeText(msgs[i].Content)
		if msgs[i].Name != nil {
			name := sanitizeText(*msgs[i].Name)
			msgs[i].Name = &name
		}
	}
}
