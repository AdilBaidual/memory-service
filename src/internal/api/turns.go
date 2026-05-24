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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/consolidation"
	"memory-service/internal/extraction"
	"memory-service/internal/storage"
)

// NewTurnsHandler handles POST /turns.
// ext may be nil when OPENAI_API_KEY is not set — turns are saved without extraction.
func NewTurnsHandler(pool *pgxpool.Pool, ext *extraction.Extractor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
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

		// Step 2: Save raw turn in its own transaction.
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
			"turn_id", turnID.String(),
			"session_id", req.SessionID,
			"user_id", userID,
			"message_count", len(req.Messages),
		)

		// Step 3: Extraction — skip if no user or extractor.
		if req.UserID == nil {
			writeJSON(w, http.StatusCreated, buildTurnResponse(turnID.String()))
			return
		}
		if ext == nil {
			slog.Warn("extractor not configured, skipping extraction", "request_id", reqID)
			writeJSON(w, http.StatusCreated, buildTurnResponse(turnID.String()))
			return
		}

		turnMsgs := make([]storage.TurnMessage, len(req.Messages))
		for i, m := range req.Messages {
			turnMsgs[i] = storage.TurnMessage{Role: m.Role, Content: m.Content}
		}

		llmCtx, llmCancel := context.WithTimeout(ctx, 30*time.Second)
		defer llmCancel()
		candidates, relationships, err := ext.Extract(llmCtx, pool, *req.UserID, turnMsgs)
		if err != nil {
			slog.Warn("extraction failed", "error", err, "request_id", reqID)
			writeJSON(w, http.StatusCreated, buildTurnResponse(turnID.String()))
			return
		}
		if len(candidates) == 0 && len(relationships) == 0 {
			slog.Info("no candidates extracted", "request_id", reqID)
			writeJSON(w, http.StatusCreated, buildTurnResponse(turnID.String()))
			return
		}

		// Step 4: Embed each candidate and persist in a second transaction.
		tx2, err := pool.Begin(ctx)
		if err != nil {
			slog.Error("begin memory transaction", "error", err, "request_id", reqID)
			writeJSON(w, http.StatusCreated, buildTurnResponse(turnID.String()))
			return
		}
		defer tx2.Rollback(ctx) //nolint:errcheck

		inserted := 0
		var lastMemoryID uuid.UUID
		for _, c := range candidates {
			if ctx.Err() != nil {
				break
			}
			embCtx, embCancel := context.WithTimeout(ctx, 10*time.Second)
			embedding, embErr := ext.Embed(embCtx, c.Value)
			embCancel()
			if embErr != nil {
				slog.Warn("embed candidate failed, storing without vector",
					"error", embErr, "request_id", reqID)
				embedding = nil
			}

			switch c.Type {
			case "fact", "preference":
				id, result, consErr := consolidation.ConsolidateFact(ctx, tx2,
					*req.UserID, c.Type, c.Key, c.Value, c.Evidence, c.Confidence,
					c.Entities, embedding, &req.SessionID, &turnID,
				)
				if consErr != nil {
					slog.Warn("consolidation failed",
						"type", c.Type, "key", c.Key, "err", consErr, "request_id", reqID)
					continue
				}
				if result != consolidation.ResultNOOP {
					lastMemoryID = id
				}
				inserted++
				slog.Debug("memory consolidated",
					"result", result.String(), "type", c.Type, "key", c.Key, "user_id", *req.UserID)

			case "opinion", "event":
				entJSON, _ := json.Marshal(c.Entities)
				id, insErr := storage.InsertMemory(ctx, tx2, storage.InsertMemoryParams{
					UserID:        *req.UserID,
					Type:          c.Type,
					Key:           c.Key,
					Value:         c.Value,
					Evidence:      c.Evidence,
					Confidence:    c.Confidence,
					Entities:      json.RawMessage(entJSON),
					Embedding:     embedding,
					SourceSession: &req.SessionID,
					SourceTurn:    &turnID,
					Supersedes:    nil,
				})
				if insErr != nil {
					slog.Warn("insert failed",
						"type", c.Type, "key", c.Key, "err", insErr, "request_id", reqID)
					continue
				}
				lastMemoryID = id
				inserted++
				slog.Debug("memory inserted", "type", c.Type, "key", c.Key, "user_id", *req.UserID)
			}
		}

		// Process relationship triplets after all memories are saved.
		if len(relationships) > 0 {
			if err := consolidation.ProcessRelationships(
				ctx, tx2,
				*req.UserID,
				relationships,
				lastMemoryID,
			); err != nil {
				// Non-fatal: relationships are navigation-only data.
				slog.Warn("relationship processing failed",
					"err", err,
					"user_id", *req.UserID,
					"count", len(relationships))
			} else {
				slog.Debug("relationships processed",
					"count", len(relationships),
					"user_id", *req.UserID)
			}
		}

		if err := tx2.Commit(ctx); err != nil {
			slog.Error("commit memory transaction", "error", err, "request_id", reqID)
		} else {
			slog.Info("memories inserted",
				"count", inserted,
				"turn_id", turnID.String(),
				"user_id", *req.UserID,
			)
		}

		writeJSON(w, http.StatusCreated, buildTurnResponse(turnID.String()))
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

func marshalEntities(entities []string) json.RawMessage {
	if len(entities) == 0 {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(entities)
	if err != nil {
		return json.RawMessage("[]")
	}
	return json.RawMessage(b)
}
