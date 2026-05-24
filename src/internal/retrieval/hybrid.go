// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"memory-service/internal/llm"
	"memory-service/internal/storage"
)

// candidatesPerChannel is how many results each channel fetches before fusion.
const candidatesPerChannel = 30

// HybridRetriever runs semantic (cosine) and keyword (FTS) channels in parallel
// and fuses results with RRF. Stage 4: two channels.
type HybridRetriever struct {
	pool   storage.Querier
	client *llm.Client
}

// NewHybridRetriever creates a HybridRetriever.
func NewHybridRetriever(pool storage.Querier, client *llm.Client) *HybridRetriever {
	return &HybridRetriever{pool: pool, client: client}
}

// Retrieve runs semantic and keyword channels concurrently, fuses via RRF, and
// returns up to params.Limit memories.
func (r *HybridRetriever) Retrieve(
	ctx context.Context,
	params RetrieveParams,
) ([]RetrievedMemory, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}

	var (
		semanticResults []storage.ScoredMemory
		keywordResults  []storage.ScoredMemory
	)

	g, gctx := errgroup.WithContext(ctx)

	if r.client != nil {
		g.Go(func() error {
			emb, err := r.client.Embed(gctx, params.Query)
			if err != nil {
				slog.Warn("semantic channel failed", "err", err)
				return nil
			}
			results, err := storage.GetTopKByCosine(
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

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var inputs []RRFInput
	if len(semanticResults) > 0 {
		slog.Debug("[DEBUG] semanticResults", "semanticResults", semanticResults)
		inputs = append(inputs, RRFInput{Memories: semanticResults})
	}
	if len(keywordResults) > 0 {
		slog.Debug("[DEBUG] keywordResults", "keywordResults", keywordResults)
		inputs = append(inputs, RRFInput{Memories: keywordResults})
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
