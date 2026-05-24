// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
)

const (
	candidatesPerChannel = 30
	rerankCandidates     = 30

	// temporalLambda controls decay rate: half-life ≈ 139 days.
	// Conservative — stable facts stay relevant even if not updated recently.
	temporalLambda = 0.005
	// temporalAlpha is the weight of the original RRF score.
	// Recency can add at most (1-alpha)=30% boost but cannot override semantic relevance.
	temporalAlpha = 0.7
)

// HybridRetriever runs semantic (cosine), keyword (FTS), and graph channels
// in parallel, fuses results with RRF, applies temporal boost, then optionally
// reranks with Cohere cross-encoder.
type HybridRetriever struct {
	pool   store.Querier
	client *llm.Client
	cohere *llm.CohereClient
}

func NewHybridRetriever(pool store.Querier, client *llm.Client, cohere *llm.CohereClient) *HybridRetriever {
	return &HybridRetriever{pool: pool, client: client, cohere: cohere}
}

// Retrieve runs semantic, keyword, and graph channels concurrently, fuses via
// RRF, applies temporal recency boost, optionally reranks with Cohere, and
// returns up to params.Limit memories.
func (r *HybridRetriever) Retrieve(
	ctx context.Context,
	params RetrieveParams,
) ([]RetrievedMemory, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}

	var (
		semanticResults []store.ScoredMemory
		keywordResults  []store.ScoredMemory
		graphResults    []store.ScoredMemory
	)

	g, gctx := errgroup.WithContext(ctx)

	if r.client != nil {
		g.Go(func() error {
			emb, err := r.client.Embed(gctx, params.Query)
			if err != nil {
				slog.Warn("semantic channel failed", "err", err)
				return nil
			}
			results, err := store.GetTopKByCosine(
				gctx, r.pool, params.UserID, emb, candidatesPerChannel)
			if err != nil {
				return fmt.Errorf("semantic search: %w", err)
			}
			semanticResults = results
			return nil
		})
	}

	g.Go(func() error {
		results, err := keywordSearch(
			gctx, r.pool, params.UserID, params.Query, candidatesPerChannel)
		if err != nil {
			slog.Warn("keyword channel failed", "err", err)
			return nil
		}
		keywordResults = results
		return nil
	})

	g.Go(func() error {
		results, err := graphSearch(
			gctx, r.pool, params.UserID, params.Query, candidatesPerChannel)
		if err != nil {
			slog.Warn("graph channel failed", "err", err)
			return nil
		}
		graphResults = results
		return nil
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var inputs []RRFInput
	if len(semanticResults) > 0 {
		inputs = append(inputs, RRFInput{Memories: semanticResults})
	}
	if len(keywordResults) > 0 {
		inputs = append(inputs, RRFInput{Memories: keywordResults})
	}
	if len(graphResults) > 0 {
		inputs = append(inputs, RRFInput{Memories: graphResults})
	}

	if len(inputs) == 0 {
		return nil, nil
	}

	// Pass enough candidates for reranker; final slice is trimmed to Limit below.
	fuseCandidates := rerankCandidates
	if params.Limit > fuseCandidates {
		fuseCandidates = params.Limit
	}
	fused := FuseRRF(inputs, fuseCandidates)

	boosted := applyTemporalBoost(fused, time.Now())
	reranked := r.applyReranker(ctx, params.Query, boosted, params.Limit)

	if len(reranked) > params.Limit {
		reranked = reranked[:params.Limit]
	}

	result := make([]RetrievedMemory, len(reranked))
	for i, m := range reranked {
		result[i] = RetrievedMemory{ScoredMemory: m}
	}
	return result, nil
}

// applyTemporalBoost adjusts RRF scores based on memory recency.
// final = rrf_score * (alpha + (1-alpha) * exp(-lambda * days_since_update))
func applyTemporalBoost(memories []store.ScoredMemory, now time.Time) []store.ScoredMemory {
	out := make([]store.ScoredMemory, len(memories))
	for i, m := range memories {
		days := now.Sub(m.UpdatedAt).Hours() / 24
		recency := math.Exp(-temporalLambda * days)
		m.Score = m.Score * float32(temporalAlpha+(1-temporalAlpha)*recency)
		out[i] = m
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}

// applyReranker reranks fused results using Cohere cross-encoder.
// Returns original order if cohere client is nil or on error.
func (r *HybridRetriever) applyReranker(
	ctx context.Context,
	query string,
	memories []store.ScoredMemory,
	finalLimit int,
) []store.ScoredMemory {
	if r.cohere == nil || len(memories) == 0 {
		return memories
	}

	docs := make([]string, len(memories))
	for i, m := range memories {
		docs[i] = m.Value
	}

	results, err := r.cohere.Rerank(ctx, query, docs, finalLimit)
	if err != nil {
		slog.Warn("cohere rerank failed, using RRF order", "err", err)
		return memories
	}
	if len(results) == 0 {
		return memories
	}

	slog.Info("cohere rerank applied", "candidates", len(memories), "returned", len(results))

	reranked := make([]store.ScoredMemory, len(results))
	for i, res := range results {
		m := memories[res.Index]
		m.Score = res.RelevanceScore
		reranked[i] = m
	}
	return reranked
}
