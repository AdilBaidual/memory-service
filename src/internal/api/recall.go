// Package api contains the HTTP router, handlers, and middleware for the memory service.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/retrieval"
)

// NewRecallHandler handles POST /recall.
// ret may be nil when OPENAI_API_KEY is not set — returns cold-session response.
func NewRecallHandler(pool *pgxpool.Pool, ret retrieval.Retriever) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		reqID := chimiddleware.GetReqID(r.Context())
		_ = pool // reserved for future retrieval channels (FTS, graph)

		req, err := parseRecallRequest(r)
		if err != nil {
			writeError(w, err)
			return
		}

		// No user — nothing to recall.
		if req.UserID == nil {
			writeJSON(w, http.StatusOK, buildRecallResponse())
			return
		}

		// No retriever configured — graceful degradation.
		if ret == nil {
			writeJSON(w, http.StatusOK, buildRecallResponse())
			return
		}

		memories, err := ret.Retrieve(ctx, retrieval.RetrieveParams{
			Query:  req.Query,
			UserID: *req.UserID,
			Limit:  10,
		})
		if err != nil {
			slog.Warn("retrieval failed", "error", err, "request_id", reqID)
			writeJSON(w, http.StatusOK, buildRecallResponse())
			return
		}

		writeJSON(w, http.StatusOK, RecallResponse{
			Context:   buildSimpleContext(memories),
			Citations: buildCitations(memories),
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

func buildRecallResponse() RecallResponse {
	return RecallResponse{
		Context:   "",
		Citations: []Citation{},
	}
}

// buildSimpleContext concatenates memory values into a plain text context string.
func buildSimpleContext(memories []retrieval.RetrievedMemory) string {
	if len(memories) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Known information about this user:\n")
	for _, m := range memories {
		sb.WriteString("- ")
		if m.Key != nil {
			sb.WriteString(*m.Key)
			sb.WriteString(": ")
		}
		sb.WriteString(m.Value)
		sb.WriteString("\n")
	}
	return sb.String()
}

// buildCitations produces deduplicated citations from retrieved memories.
func buildCitations(memories []retrieval.RetrievedMemory) []Citation {
	seen := make(map[string]bool)
	citations := []Citation{}
	for _, m := range memories {
		if m.SourceTurn == nil {
			continue
		}
		turnID := m.SourceTurn.String()
		if seen[turnID] {
			continue
		}
		seen[turnID] = true
		citations = append(citations, Citation{
			TurnID:  turnID,
			Score:   m.Score,
			Snippet: truncateString(m.Value, 120),
		})
	}
	return citations
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
