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
}

type MemoriesInput struct {
	UserID  string
	Filters store.ListMemoriesFilters
}

type MemoriesOutput struct {
	Memories []store.Memory
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
	return MemoriesOutput{Memories: mems}, nil
}
