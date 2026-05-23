// Package retrieval implements the hybrid retrieval pipeline: semantic, keyword,
// graph, and temporal channels fused via Reciprocal Rank Fusion and cross-encoder reranking.
package retrieval

import (
	"context"

	"memory-service/internal/storage"
)

// RetrievedMemory is a scored memory returned by the retrieval pipeline.
type RetrievedMemory struct {
	storage.ScoredMemory
}

// RetrieveParams holds parameters for a retrieval request.
type RetrieveParams struct {
	Query  string
	UserID string
	Limit  int
}

// Retriever is the interface for the retrieval pipeline.
// Stage 3: only the semantic (cosine) channel is implemented.
// Stage 4+: adds FTS, graph traversal, RRF fusion, and cross-encoder reranking.
type Retriever interface {
	// Retrieve returns up to Limit memories relevant to Query for the given user.
	Retrieve(ctx context.Context, params RetrieveParams) ([]RetrievedMemory, error)
}
