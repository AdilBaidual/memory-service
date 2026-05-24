// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"log/slog"
	"math"
	"sort"

	"memory-service/internal/adapters/store"
	assembler "memory-service/internal/service/context"
	"memory-service/internal/identity"
	"memory-service/internal/service/retrieval"
)

// StableMemoryLoader loads pre-queried stable facts, events, and opinion views.
type StableMemoryLoader interface {
	GetActiveMemoriesByTypes(ctx context.Context, scope identity.Scope, types []string) ([]store.Memory, error)
	GetActiveOpinionViewsWithEmbeddings(ctx context.Context, scope identity.Scope) ([]store.OpinionViewWithEmbedding, error)
}

// QueryEmbedder embeds a text string into a float vector.
type QueryEmbedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

type Retriever interface {
	Retrieve(ctx context.Context, params retrieval.RetrieveParams) ([]retrieval.RetrievedMemory, error)
}

type Citation struct {
	TurnID  string
	Score   float32
	Snippet string
}

type RecallInput struct {
	Query     string
	UserID    *string
	SessionID string
	MaxTokens int
}

type RecallOutput struct {
	Context   string
	Citations []Citation
}

type RecallUsecase struct {
	retriever Retriever
	loader    StableMemoryLoader // may be nil
	embedder  QueryEmbedder      // may be nil
}

// NewRecallUsecase creates a RecallUsecase. All parameters may be nil.
func NewRecallUsecase(retriever Retriever, loader StableMemoryLoader, embedder QueryEmbedder) *RecallUsecase {
	return &RecallUsecase{retriever: retriever, loader: loader, embedder: embedder}
}

// Recall retrieves relevant memories and assembles structured context.
// Errors degrade gracefully to empty context.
func (uc *RecallUsecase) Recall(ctx context.Context, in RecallInput) RecallOutput {
	empty := RecallOutput{Context: "", Citations: []Citation{}}

	scope := identity.Resolve(in.UserID, in.SessionID)
	if scope.Value == "" {
		return empty
	}

	// Embed query once; used for opinion_view filtering.
	var queryEmbedding []float32
	if uc.embedder != nil {
		emb, err := uc.embedder.Embed(ctx, in.Query)
		if err != nil {
			slog.Warn("query embedding failed, skipping opinion filter", "error", err, "scope", scope.String())
		} else {
			queryEmbedding = emb
		}
	}

	// Load stable facts and recent events from DB.
	var stableFacts, recentEvents []store.Memory
	var opinionViews []store.Memory
	if uc.loader != nil {
		var err error
		stableFacts, err = uc.loader.GetActiveMemoriesByTypes(ctx, scope, []string{"fact", "preference"})
		if err != nil {
			slog.Warn("load stable facts failed", "error", err, "scope", scope.String())
		}

		ovWithEmb, err := uc.loader.GetActiveOpinionViewsWithEmbeddings(ctx, scope)
		if err != nil {
			slog.Warn("load opinion views failed", "error", err, "scope", scope.String())
		} else {
			opinionViews = filterOpinionViewsByRelevance(ovWithEmb, queryEmbedding)
		}

		recentEvents, err = uc.loader.GetActiveMemoriesByTypes(ctx, scope, []string{"event"})
		if err != nil {
			slog.Warn("load recent events failed", "error", err, "scope", scope.String())
		}
	}

	// Hybrid retrieval.
	var retrieved []retrieval.RetrievedMemory
	if uc.retriever != nil {
		var err error
		retrieved, err = uc.retriever.Retrieve(ctx, retrieval.RetrieveParams{
			Query: in.Query,
			Scope: scope,
			Limit: 25,
		})
		if err != nil {
			slog.Warn("retrieval failed", "error", err, "scope", scope.String())
		}
	}

	// Convert retrieved memories to store.Memory for the assembler.
	retrievedMems := make([]store.Memory, len(retrieved))
	for i, r := range retrieved {
		retrievedMems[i] = r.Memory
	}

	// assembler.Assemble uses UserID only for context header; use scope.Value.
	userIDForAssembler := scope.Value
	if !scope.IsUser() {
		userIDForAssembler = "" // anonymous: omit from context header
	}

	out := assembler.Assemble(assembler.AssemblyInput{
		UserID:        userIDForAssembler,
		Query:         in.Query,
		MaxTokens:     in.MaxTokens,
		StableFacts:   stableFacts,
		OpinionViews:  opinionViews,
		RetrievedMems: retrievedMems,
		RecentEvents:  recentEvents,
	})

	return RecallOutput{
		Context:   out.Context,
		Citations: buildCitations(retrieved),
	}
}

const (
	opinionViewMinScore = float32(0.25)
	opinionViewMinCount = 2
)

// filterOpinionViewsByRelevance returns only opinion_views whose stored embedding
// is above the cosine similarity threshold, always keeping at least minCount.
// Views without embeddings are included by default (safe degradation).
// When queryEmbedding is nil all views are returned unchanged.
func filterOpinionViewsByRelevance(
	views []store.OpinionViewWithEmbedding,
	queryEmbedding []float32,
) []store.Memory {
	if len(views) == 0 {
		return nil
	}
	// No embedding available — return all views unfiltered.
	if queryEmbedding == nil {
		mems := make([]store.Memory, len(views))
		for i, v := range views {
			mems[i] = v.Memory
		}
		return mems
	}
	// Only bother filtering when we have more than the guaranteed minimum.
	if len(views) <= opinionViewMinCount {
		mems := make([]store.Memory, len(views))
		for i, v := range views {
			mems[i] = v.Memory
		}
		return mems
	}

	type scored struct {
		mem   store.Memory
		score float32
	}
	results := make([]scored, 0, len(views))
	for _, v := range views {
		if v.Embedding == nil {
			// No embedding stored — include at threshold score (default include).
			results = append(results, scored{v.Memory, opinionViewMinScore})
			continue
		}
		results = append(results, scored{v.Memory, cosineSimilarity(queryEmbedding, v.Embedding)})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	var filtered []store.Memory
	for i, r := range results {
		if r.score >= opinionViewMinScore || i < opinionViewMinCount {
			filtered = append(filtered, r.mem)
		}
	}
	return filtered
}

func cosineSimilarity(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / math.Sqrt(normA*normB))
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
