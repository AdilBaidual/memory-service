// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"memory-service/internal/adapters/store"
)

// MockMemoryLister mocks MemoryLister using testify/mock.
type MockMemoryLister struct{ mock.Mock }

func (m *MockMemoryLister) ListMemoriesByUser(ctx context.Context, userID string, f store.ListMemoriesFilters) ([]store.Memory, error) {
	args := m.Called(ctx, userID, f)
	mems, _ := args.Get(0).([]store.Memory)
	return mems, args.Error(1)
}

func (m *MockMemoryLister) CountMemoriesByUser(ctx context.Context, userID string, f store.ListMemoriesFilters) (int, error) {
	args := m.Called(ctx, userID, f)
	return args.Int(0), args.Error(1)
}

func TestListMemoriesUsecase_HappyPath(t *testing.T) {
	mems := []store.Memory{{Value: "Berlin"}, {Value: "Engineer"}}
	l := new(MockMemoryLister)
	l.On("ListMemoriesByUser", mock.Anything, "u1", mock.Anything).Return(mems, nil)
	l.On("CountMemoriesByUser", mock.Anything, "u1", mock.Anything).Return(2, nil)
	uc := NewListMemoriesUsecase(l)
	out, err := uc.List(context.Background(), MemoriesInput{UserID: "u1"})
	assert.NoError(t, err)
	assert.Len(t, out.Memories, 2)
	assert.Equal(t, 2, out.Total)
	l.AssertExpectations(t)
}

func TestListMemoriesUsecase_EmptyList(t *testing.T) {
	l := new(MockMemoryLister)
	l.On("ListMemoriesByUser", mock.Anything, "u1", mock.Anything).Return([]store.Memory{}, nil)
	l.On("CountMemoriesByUser", mock.Anything, "u1", mock.Anything).Return(0, nil)
	uc := NewListMemoriesUsecase(l)
	out, err := uc.List(context.Background(), MemoriesInput{UserID: "u1"})
	assert.NoError(t, err)
	assert.Empty(t, out.Memories)
	assert.Equal(t, 0, out.Total)
	l.AssertExpectations(t)
}

func TestListMemoriesUsecase_ListerError_Propagated(t *testing.T) {
	l := new(MockMemoryLister)
	l.On("ListMemoriesByUser", mock.Anything, "u1", mock.Anything).Return(nil, errors.New("db error"))
	uc := NewListMemoriesUsecase(l)
	_, err := uc.List(context.Background(), MemoriesInput{UserID: "u1"})
	assert.ErrorContains(t, err, "list memories")
	l.AssertExpectations(t)
}
