// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
)

// candidatesPerChannel is how many results each channel fetches before fusion.
const candidatesPerChannel = 30

// HybridRetriever runs semantic (cosine), keyword (FTS), and graph channels
// in parallel and fuses results with RRF.
type HybridRetriever struct {
	pool   store.Querier
	client *llm.Client
}

// NewHybridRetriever creates a HybridRetriever.
func NewHybridRetriever(pool store.Querier, client *llm.Client) *HybridRetriever {
	return &HybridRetriever{pool: pool, client: client}
}

// Retrieve runs semantic, keyword, and graph channels concurrently, fuses via
// RRF, and returns up to params.Limit memories.
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

	fused := FuseRRF(inputs, params.Limit)

	result := make([]RetrievedMemory, len(fused))
	for i, m := range fused {
		result[i] = RetrievedMemory{ScoredMemory: m}
	}
	return result, nil
}
