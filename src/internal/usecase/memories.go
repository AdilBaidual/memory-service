// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"fmt"

	"memory-service/internal/adapters/store"
)

// MemoryLister abstracts the storage layer for listing memories.
type MemoryLister interface {
	ListMemoriesByUser(ctx context.Context, userID string, f store.ListMemoriesFilters) ([]store.Memory, error)
	CountMemoriesByUser(ctx context.Context, userID string, f store.ListMemoriesFilters) (int, error)
}

type MemoriesInput struct {
	UserID  string
	Filters store.ListMemoriesFilters
}

type MemoriesOutput struct {
	Memories []store.Memory
	Total    int
	Limit    int
	Offset   int
}

type ListMemoriesUsecase struct {
	lister MemoryLister
}

func NewListMemoriesUsecase(l MemoryLister) *ListMemoriesUsecase {
	return &ListMemoriesUsecase{lister: l}
}

func (uc *ListMemoriesUsecase) List(ctx context.Context, in MemoriesInput) (MemoriesOutput, error) {
	mems, err := uc.lister.ListMemoriesByUser(ctx, in.UserID, in.Filters)
	if err != nil {
		return MemoriesOutput{}, fmt.Errorf("list memories: %w", err)
	}

	total, err := uc.lister.CountMemoriesByUser(ctx, in.UserID, in.Filters)
	if err != nil {
		return MemoriesOutput{}, fmt.Errorf("count memories: %w", err)
	}

	limit := in.Filters.Limit
	if limit <= 0 {
		limit = 100
	}

	return MemoriesOutput{
		Memories: mems,
		Total:    total,
		Limit:    limit,
		Offset:   in.Filters.Offset,
	}, nil
}
