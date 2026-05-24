// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"memory-service/internal/adapters/store"
)

type mockMemoryLister struct {
	memories []store.Memory
	err      error
}

func (m *mockMemoryLister) ListMemoriesByUser(_ context.Context, _ string, _ store.ListMemoriesFilters) ([]store.Memory, error) {
	return m.memories, m.err
}

func TestListMemoriesUsecase_HappyPath(t *testing.T) {
	mems := []store.Memory{{Value: "Berlin"}, {Value: "Engineer"}}
	uc := NewListMemoriesUsecase(&mockMemoryLister{memories: mems})
	out, err := uc.List(context.Background(), MemoriesInput{UserID: "u1"})
	assert.NoError(t, err)
	assert.Len(t, out.Memories, 2)
}

func TestListMemoriesUsecase_EmptyList(t *testing.T) {
	uc := NewListMemoriesUsecase(&mockMemoryLister{memories: []store.Memory{}})
	out, err := uc.List(context.Background(), MemoriesInput{UserID: "u1"})
	assert.NoError(t, err)
	assert.Empty(t, out.Memories)
}

func TestListMemoriesUsecase_ListerError_Propagated(t *testing.T) {
	uc := NewListMemoriesUsecase(&mockMemoryLister{err: errors.New("db error")})
	_, err := uc.List(context.Background(), MemoriesInput{UserID: "u1"})
	assert.ErrorContains(t, err, "list memories")
}
