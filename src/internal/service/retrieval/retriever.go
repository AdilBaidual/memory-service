// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"

	"memory-service/internal/adapters/store"
)

type RetrievedMemory struct {
	store.ScoredMemory
}

type RetrieveParams struct {
	Query  string
	UserID string
	Limit  int
}

type Retriever interface {
	Retrieve(ctx context.Context, params RetrieveParams) ([]RetrievedMemory, error)
}
