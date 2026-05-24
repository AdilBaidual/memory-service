// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"memory-service/internal/adapters/store"
	"memory-service/internal/service/retrieval"
)

type mockRetriever struct {
	memories []retrieval.RetrievedMemory
	err      error
	called   bool
}

func (m *mockRetriever) Retrieve(_ context.Context, _ retrieval.RetrieveParams) ([]retrieval.RetrievedMemory, error) {
	m.called = true
	return m.memories, m.err
}

func TestRecallUsecase_NoUserID_ReturnsEmpty(t *testing.T) {
	uc := NewRecallUsecase(&mockRetriever{})
	out := uc.Recall(context.Background(), RecallInput{Query: "test"})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
}

func TestRecallUsecase_NilRetriever_ReturnsEmpty(t *testing.T) {
	uc := NewRecallUsecase(nil)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "test", UserID: &uid})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
}

func TestRecallUsecase_RetrieverError_ReturnsEmpty(t *testing.T) {
	r := &mockRetriever{err: errors.New("db error")}
	uc := NewRecallUsecase(r)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "test", UserID: &uid})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
	assert.True(t, r.called)
}

func TestRecallUsecase_NoMemories_ReturnsEmpty(t *testing.T) {
	r := &mockRetriever{memories: []retrieval.RetrievedMemory{}}
	uc := NewRecallUsecase(r)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "test", UserID: &uid})
	assert.Empty(t, out.Context)
	assert.Equal(t, []Citation{}, out.Citations)
}

func TestRecallUsecase_HappyPath_BuildsContext(t *testing.T) {
	turnID := uuid.New()
	key := "name"
	session := "session-1"
	r := &mockRetriever{
		memories: []retrieval.RetrievedMemory{
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
		},
	}
	uc := NewRecallUsecase(r)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "name", UserID: &uid})
	assert.Contains(t, out.Context, "Alice")
	assert.Len(t, out.Citations, 1)
	assert.Equal(t, turnID.String(), out.Citations[0].TurnID)
}

func TestRecallUsecase_DuplicateTurns_DeduplicatesCitations(t *testing.T) {
	turnID := uuid.New()
	key := "city"
	session := "s1"
	r := &mockRetriever{
		memories: []retrieval.RetrievedMemory{
			{ScoredMemory: store.ScoredMemory{Memory: store.Memory{Key: &key, Value: "Berlin", SourceTurn: &turnID, SourceSession: &session, CreatedAt: time.Now()}, Score: 0.9}},
			{ScoredMemory: store.ScoredMemory{Memory: store.Memory{Key: &key, Value: "Germany", SourceTurn: &turnID, SourceSession: &session, CreatedAt: time.Now()}, Score: 0.8}},
		},
	}
	uc := NewRecallUsecase(r)
	uid := "user-1"
	out := uc.Recall(context.Background(), RecallInput{Query: "location", UserID: &uid})
	assert.Len(t, out.Citations, 1, "same turn_id should appear only once")
}

func TestTruncateString_ShortString_Unchanged(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
}

func TestTruncateString_LongString_Truncated(t *testing.T) {
	result := truncateString("hello world", 5)
	assert.Equal(t, "hello", result)
}
