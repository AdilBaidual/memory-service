// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/retrieval"
)

// NewSearchHandler handles POST /search.
// ret may be nil when OPENAI_API_KEY is not set — returns empty results.
func NewSearchHandler(pool *pgxpool.Pool, ret retrieval.Retriever) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		reqID := chimiddleware.GetReqID(r.Context())
		_ = pool // reserved for future retrieval channels

		req, err := parseSearchRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		// No user scope — nothing to search.
		if req.UserID == nil {
			writeJSON(w, http.StatusOK, buildSearchResponse())
			return
		}

		// No retriever configured — graceful degradation.
		if ret == nil {
			writeJSON(w, http.StatusOK, buildSearchResponse())
			return
		}

		memories, err := ret.Retrieve(ctx, retrieval.RetrieveParams{
			Query:  req.Query,
			UserID: *req.UserID,
			Limit:  req.Limit,
		})
		if err != nil {
			slog.Warn("search retrieval failed", "error", err, "request_id", reqID)
			writeJSON(w, http.StatusOK, buildSearchResponse())
			return
		}

		results := make([]SearchResult, 0, len(memories))
		for _, m := range memories {
			results = append(results, SearchResult{
				Content:   m.Value,
				Score:     m.Score,
				SessionID: derefOrEmpty(m.SourceSession),
				Timestamp: m.CreatedAt,
				Metadata:  m.Metadata,
			})
		}

		writeJSON(w, http.StatusOK, SearchResponse{Results: results})
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

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
