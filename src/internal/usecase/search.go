// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"memory-service/internal/adapters/store"
	"memory-service/internal/service/retrieval"
)

// SessionQuerier returns memories scoped to a single session.
type SessionQuerier interface {
	GetMemoriesBySession(ctx context.Context, sessionID string, limit int) ([]store.Memory, error)
}

type SearchInput struct {
	Query     string
	UserID    *string
	SessionID *string
	Limit     int
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
	retriever      Retriever
	sessionQuerier SessionQuerier // may be nil
}

// NewSearchUsecase creates a SearchUsecase. Both parameters may be nil.
func NewSearchUsecase(retriever Retriever, sessionQuerier SessionQuerier) *SearchUsecase {
	return &SearchUsecase{retriever: retriever, sessionQuerier: sessionQuerier}
}

// Search retrieves memories matching the query. Errors degrade gracefully to empty results.
func (uc *SearchUsecase) Search(ctx context.Context, in SearchInput) SearchOutput {
	empty := SearchOutput{Results: []SearchResultItem{}}

	switch {
	case in.UserID != nil && in.SessionID != nil:
		// User + session: retrieve for user, filter to session.
		if uc.retriever == nil {
			return empty
		}
		memories, err := uc.retriever.Retrieve(ctx, retrieval.RetrieveParams{
			Query:  in.Query,
			UserID: *in.UserID,
			Limit:  in.Limit * 3, // over-fetch to compensate for session filter
		})
		if err != nil {
			slog.Warn("search retrieval failed", "error", err, "user_id", *in.UserID)
			return empty
		}
		var filtered []retrieval.RetrievedMemory
		for _, m := range memories {
			if m.SourceSession != nil && *m.SourceSession == *in.SessionID {
				filtered = append(filtered, m)
				if len(filtered) >= in.Limit {
					break
				}
			}
		}
		return SearchOutput{Results: toSearchResults(filtered)}

	case in.UserID != nil:
		// User only: standard hybrid retrieval.
		if uc.retriever == nil {
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
		return SearchOutput{Results: toSearchResults(memories)}

	case in.SessionID != nil:
		// Session only: direct DB query, no embedding needed.
		if uc.sessionQuerier == nil {
			return empty
		}
		mems, err := uc.sessionQuerier.GetMemoriesBySession(ctx, *in.SessionID, in.Limit)
		if err != nil {
			slog.Warn("session search failed", "error", err, "session_id", *in.SessionID)
			return empty
		}
		return SearchOutput{Results: toSessionResults(mems)}

	default:
		return empty
	}
}

func toSearchResults(memories []retrieval.RetrievedMemory) []SearchResultItem {
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
	return results
}

func toSessionResults(mems []store.Memory) []SearchResultItem {
	results := make([]SearchResultItem, 0, len(mems))
	for _, m := range mems {
		meta := m.Metadata
		if meta == nil {
			meta = json.RawMessage("{}")
		}
		results = append(results, SearchResultItem{
			Content:   m.Value,
			Score:     m.Confidence,
			SessionID: derefString(m.SourceSession),
			Timestamp: m.CreatedAt,
			Metadata:  meta,
		})
	}
	return results
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
