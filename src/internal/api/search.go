// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewSearchHandler handles POST /search (stub — returns cold-session empty response).
func NewSearchHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req, err := parseSearchRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}
		_ = req

		writeJSON(w, http.StatusOK, buildSearchResponse())
	}
}

func parseSearchRequest(r *http.Request) (*SearchRequest, error) {
	var req SearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, &apiError{http.StatusBadRequest, "invalid request body: " + err.Error()}
	}
	if req.Query == "" {
		return nil, &apiError{http.StatusBadRequest, "query is required"}
	}
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.Limit > 50 {
		req.Limit = 50
	}
	return &req, nil
}

func buildSearchResponse() SearchResponse {
	return SearchResponse{Results: []SearchResult{}}
}
