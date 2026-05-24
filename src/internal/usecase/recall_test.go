// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"memory-service/internal/adapters/store"
	"memory-service/internal/service/retrieval"
)

// MockRetriever mocks the Retriever interface using testify/mock.
// Shared by recall_test.go and search_test.go (same package).
type MockRetriever struct{ mock.Mock }

func (m *MockRetriever) Retrieve(ctx context.Context, params retrieval.RetrieveParams) ([]retrieval.RetrievedMemory, error) {
	args := m.Called(ctx, params)
	mems, _ := args.Get(0).([]retrieval.RetrievedMemory)
	return mems, args.Error(1)
}

func TestRecallUsecase_NoUserID_ReturnsEmpty(t *testing.T) {
	r := new(MockRetriever)
	uc := NewRecallUsecase(r, nil, nil)
	out := uc.Recall(context.Background(), RecallInput{Query: "test"})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
	r.AssertNotCalled(t, "Retrieve")
}

func TestRecallUsecase_NilRetriever_ReturnsEmpty(t *testing.T) {
	uc := NewRecallUsecase(nil, nil, nil)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "test", UserID: &uid})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
}

func TestRecallUsecase_RetrieverError_ReturnsEmpty(t *testing.T) {
	r := new(MockRetriever)
	r.On("Retrieve", mock.Anything, mock.Anything).Return(nil, errors.New("db error"))
	uc := NewRecallUsecase(r, nil, nil)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "test", UserID: &uid})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
	r.AssertExpectations(t)
}

func TestRecallUsecase_NoMemories_ReturnsEmpty(t *testing.T) {
	r := new(MockRetriever)
	r.On("Retrieve", mock.Anything, mock.Anything).Return([]retrieval.RetrievedMemory{}, nil)
	uc := NewRecallUsecase(r, nil, nil)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "test", UserID: &uid})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
	r.AssertExpectations(t)
}

func TestRecallUsecase_HappyPath_BuildsContext(t *testing.T) {
	turnID := uuid.New()
	key := "name"
	session := "session-1"
	mems := []retrieval.RetrievedMemory{
		{ScoredMemory: store.ScoredMemory{
			Memory: store.Memory{
				Key:           &key,
				Value:         "Alice",
				SourceTurn:    &turnID,
				SourceSession: &session,
				CreatedAt:     time.Now(),
			},
			Score: 0.9,
		}},
	}
	r := new(MockRetriever)
	r.On("Retrieve", mock.Anything, mock.Anything).Return(mems, nil)
	uc := NewRecallUsecase(r, nil, nil)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "name", UserID: &uid})
	assert.Contains(t, out.Context, "Alice")
	assert.Len(t, out.Citations, 1)
	assert.Equal(t, turnID.String(), out.Citations[0].TurnID)
	r.AssertExpectations(t)
}

func TestRecallUsecase_DuplicateTurns_DeduplicatesCitations(t *testing.T) {
	turnID := uuid.New()
	key := "city"
	session := "s1"
	mems := []retrieval.RetrievedMemory{
		{ScoredMemory: store.ScoredMemory{Memory: store.Memory{Key: &key, Value: "Berlin", SourceTurn: &turnID, SourceSession: &session, CreatedAt: time.Now()}, Score: 0.9}},
		{ScoredMemory: store.ScoredMemory{Memory: store.Memory{Key: &key, Value: "Germany", SourceTurn: &turnID, SourceSession: &session, CreatedAt: time.Now()}, Score: 0.8}},
	}
	r := new(MockRetriever)
	r.On("Retrieve", mock.Anything, mock.Anything).Return(mems, nil)
	uc := NewRecallUsecase(r, nil, nil)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "location", UserID: &uid})
	assert.Len(t, out.Citations, 1, "same turn_id should appear only once")
	r.AssertExpectations(t)
}

func TestTruncateString_ShortString_Unchanged(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
}

func TestTruncateString_LongString_Truncated(t *testing.T) {
	result := truncateString("hello world", 5)
	assert.Equal(t, "hello", result)
}
