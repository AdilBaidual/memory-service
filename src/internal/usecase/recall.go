// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"log/slog"
	"strings"

	"memory-service/internal/service/retrieval"
)

// Retriever retrieves memories relevant to a query for a given user.
type Retriever interface {
	Retrieve(ctx context.Context, params retrieval.RetrieveParams) ([]retrieval.RetrievedMemory, error)
}

// Citation references a source turn that contributed to a recall response.
type Citation struct {
	TurnID  string
	Score   float32
	Snippet string
}

// RecallInput carries the parameters for a recall operation.
type RecallInput struct {
	Query     string
	UserID    *string
	SessionID string
	MaxTokens int
}

// RecallOutput carries the result of a recall operation.
type RecallOutput struct {
	Context   string
	Citations []Citation
}

// RecallUsecase retrieves context for an agent's next turn.
type RecallUsecase struct {
	retriever Retriever
}

// NewRecallUsecase creates a RecallUsecase. retriever may be nil.
func NewRecallUsecase(retriever Retriever) *RecallUsecase {
	return &RecallUsecase{retriever: retriever}
}

// Recall retrieves relevant memories and assembles a context string and citations.
// Errors degrade gracefully to empty context.
func (uc *RecallUsecase) Recall(ctx context.Context, in RecallInput) RecallOutput {
	empty := RecallOutput{Context: "", Citations: []Citation{}}

	if in.UserID == nil || uc.retriever == nil {
		return empty
	}

	memories, err := uc.retriever.Retrieve(ctx, retrieval.RetrieveParams{
		Query:  in.Query,
		UserID: *in.UserID,
		Limit:  10,
	})
	if err != nil {
		slog.Warn("retrieval failed", "error", err, "user_id", *in.UserID)
		return empty
	}

	return RecallOutput{
		Context:   buildSimpleContext(memories),
		Citations: buildCitations(memories),
	}
}

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

// truncateString caps s at max runes to avoid splitting multi-byte sequences.
func truncateString(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
