// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRecallHandler handles POST /recall (stub — returns cold-session empty response).
func NewRecallHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := parseRecallRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		_ = req

		writeJSON(w, http.StatusOK, buildRecallResponse())
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

func buildRecallResponse() RecallResponse {
	return RecallResponse{
		Context:   "",
		Citations: []Citation{},
	}
}
