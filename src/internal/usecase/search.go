// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"memory-service/internal/service/retrieval"
)

type SearchInput struct {
	Query  string
	UserID *string
	Limit  int
}

type SearchResultItem struct {
	Content   string
	Score     float32
	SessionID string
	Timestamp time.Time
	Metadata  json.RawMessage
}

type SearchOutput struct {
	Results []SearchResultItem
}

type SearchUsecase struct {
	retriever Retriever
}

// NewSearchUsecase creates a SearchUsecase. retriever may be nil.
func NewSearchUsecase(retriever Retriever) *SearchUsecase {
	return &SearchUsecase{retriever: retriever}
}

// Search retrieves memories matching the query. Errors degrade gracefully to empty results.
func (uc *SearchUsecase) Search(ctx context.Context, in SearchInput) SearchOutput {
	empty := SearchOutput{Results: []SearchResultItem{}}

	if in.UserID == nil || uc.retriever == nil {
		return empty
	}

	memories, err := uc.retriever.Retrieve(ctx, retrieval.RetrieveParams{
		Query:  in.Query,
		UserID: *in.UserID,
		Limit:  in.Limit,
	})
	if err != nil {
		slog.Warn("search retrieval failed", "error", err, "user_id", *in.UserID)
		return empty
	}

	results := make([]SearchResultItem, 0, len(memories))
	for _, m := range memories {
		results = append(results, SearchResultItem{
			Content:   m.Value,
			Score:     m.Score,
			SessionID: derefString(m.SourceSession),
			Timestamp: m.CreatedAt,
			Metadata:  m.Metadata,
		})
	}
	return SearchOutput{Results: results}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
