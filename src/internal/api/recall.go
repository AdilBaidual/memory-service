package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"memory-service/internal/usecase"
)

// NewRecallHandler handles POST /recall.
func NewRecallHandler(uc *usecase.RecallUsecase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		req, err := parseRecallRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		out := uc.Recall(ctx, usecase.RecallInput{
			Query:     req.Query,
			UserID:    req.UserID,
			SessionID: req.SessionID,
			MaxTokens: req.MaxTokens,
		})

		writeJSON(w, http.StatusOK, RecallResponse{
			Context:   out.Context,
			Citations: toAPICitations(out.Citations),
		})
	}
}

func parseRecallRequest(r *http.Request) (*RecallRequest, error) {
	var req RecallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, &apiError{http.StatusBadRequest, "invalid request body: " + err.Error()}
	}
	if req.Query == "" {
		return nil, &apiError{http.StatusBadRequest, "query is required"}
	}
	if req.SessionID == "" {
		return nil, &apiError{http.StatusBadRequest, "session_id is required"}
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 1024
	}
	return &req, nil
}

func toAPICitations(cits []usecase.Citation) []Citation {
	out := make([]Citation, len(cits))
	for i, c := range cits {
		out[i] = Citation{TurnID: c.TurnID, Score: c.Score, Snippet: c.Snippet}
	}
	return out
}
