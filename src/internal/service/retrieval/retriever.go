// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"

	"memory-service/internal/adapters/store"
)

// RetrievedMemory is a scored memory returned by the retrieval pipeline.
type RetrievedMemory struct {
	store.ScoredMemory
}

// RetrieveParams holds parameters for a retrieval request.
type RetrieveParams struct {
	Query  string
	UserID string
	Limit  int
}

// Retriever is the interface for the retrieval pipeline.
type Retriever interface {
	// Retrieve returns up to Limit memories relevant to Query for the given user.
	Retrieve(ctx context.Context, params RetrieveParams) ([]RetrievedMemory, error)
}
