// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/adapters/store"
)

// MemoriesInput carries the parameters for listing memories.
type MemoriesInput struct {
	UserID  string
	Filters store.ListMemoriesFilters
}

// MemoriesOutput carries the list of memories.
type MemoriesOutput struct {
	Memories []store.Memory
}

// ListMemoriesUsecase handles listing memories for a user.
type ListMemoriesUsecase struct {
	pool *pgxpool.Pool
}

func NewListMemoriesUsecase(pool *pgxpool.Pool) *ListMemoriesUsecase {
	return &ListMemoriesUsecase{pool: pool}
}

// List returns memories for the specified user.
func (uc *ListMemoriesUsecase) List(ctx context.Context, in MemoriesInput) (MemoriesOutput, error) {
	mems, err := store.ListMemoriesByUser(ctx, uc.pool, in.UserID, in.Filters)
	if err != nil {
		return MemoriesOutput{}, fmt.Errorf("list memories: %w", err)
	}
	return MemoriesOutput{Memories: mems}, nil
}
