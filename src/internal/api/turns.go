package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"memory-service/internal/usecase"
)

// NewTurnsHandler handles POST /turns.
func NewTurnsHandler(uc *usecase.IngestTurnUsecase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()

		req, err := parseTurnRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		sanitizeTurnMessages(req.Messages)

		out, err := uc.Ingest(ctx, usecase.TurnInput{
			SessionID: req.SessionID,
			UserID:    req.UserID,
			Messages:  toUsecaseMessages(req.Messages),
			Timestamp: req.Timestamp,
			Metadata:  req.Metadata,
		})
		if err != nil {
			writeError(w, fmt.Errorf("ingest turn: %w", err))
			return
		}

		writeJSON(w, http.StatusCreated, buildTurnResponse(out.ID))
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

func sanitizeTurnMessages(msgs []Message) {
	for i := range msgs {
		msgs[i].Content = sanitizeText(msgs[i].Content)
		if msgs[i].Name != nil {
			name := sanitizeText(*msgs[i].Name)
			msgs[i].Name = &name
		}
	}
}

func toUsecaseMessages(msgs []Message) []usecase.TurnMessage {
	out := make([]usecase.TurnMessage, len(msgs))
	for i, m := range msgs {
		out[i] = usecase.TurnMessage{Role: m.Role, Content: m.Content, Name: m.Name}
	}
	return out
}
