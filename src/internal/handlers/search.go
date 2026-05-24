package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"memory-service/internal/usecase"
)

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	req, err := parseSearchRequest(r)
	if err != nil {
		writeError(w, err)
		return
	}

	out := h.search.Search(ctx, usecase.SearchInput{
		Query:     req.Query,
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Limit:     req.Limit,
	})

	results := make([]SearchResult, len(out.Results))
	for i, item := range out.Results {
		results[i] = SearchResult{
			Content:   item.Content,
			Score:     item.Score,
			SessionID: item.SessionID,
			Timestamp: item.Timestamp,
			Metadata:  item.Metadata,
		}
	}
	writeJSON(w, http.StatusOK, SearchResponse{Results: results})
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
