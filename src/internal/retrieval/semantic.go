package retrieval

import (
	"context"
	"fmt"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
)

// SemanticRetriever retrieves memories using cosine similarity on OpenAI embeddings.
type SemanticRetriever struct {
	pool   store.Querier
	client *llm.Client
}

// NewSemanticRetriever creates a SemanticRetriever.
func NewSemanticRetriever(pool store.Querier, client *llm.Client) *SemanticRetriever {
	return &SemanticRetriever{pool: pool, client: client}
}

// Retrieve embeds the query and returns the top-k nearest active memories by cosine distance.
func (r *SemanticRetriever) Retrieve(ctx context.Context, params RetrieveParams) ([]RetrievedMemory, error) {
	if r.client == nil {
		return nil, nil
	}

	if params.Limit <= 0 {
		params.Limit = 10
	}

	queryEmbedding, err := r.client.Embed(ctx, params.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	scored, err := store.GetTopKByCosine(ctx, r.pool, params.UserID, queryEmbedding, params.Limit)
	if err != nil {
		return nil, fmt.Errorf("cosine search: %w", err)
	}

	result := make([]RetrievedMemory, len(scored))
	for i, m := range scored {
		result[i] = RetrievedMemory{ScoredMemory: m}
	}
	return result, nil
}
