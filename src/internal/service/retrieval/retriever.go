// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"

	"memory-service/internal/adapters/store"
	"memory-service/internal/identity"
)

type RetrievedMemory struct {
	store.ScoredMemory
}

type RetrieveParams struct {
	Query string
	Scope identity.Scope
	Limit int
}

type Retriever interface {
	Retrieve(ctx context.Context, params RetrieveParams) ([]RetrievedMemory, error)
}
