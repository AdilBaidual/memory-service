// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"memory-service/internal/adapters/store"
	"memory-service/internal/service/retrieval"
)

func TestSearchUsecase_NoUserID_ReturnsEmpty(t *testing.T) {
	r := new(MockRetriever)
	uc := NewSearchUsecase(r)
	out := uc.Search(context.Background(), SearchInput{Query: "test"})
	assert.Equal(t, []SearchResultItem{}, out.Results)
	r.AssertNotCalled(t, "Retrieve")
}

func TestSearchUsecase_NilRetriever_ReturnsEmpty(t *testing.T) {
	uc := NewSearchUsecase(nil)
	uid := "user-1"
	out := uc.Search(context.Background(), SearchInput{Query: "test", UserID: &uid})
	assert.Equal(t, []SearchResultItem{}, out.Results)
}

func TestSearchUsecase_RetrieverError_ReturnsEmpty(t *testing.T) {
	r := new(MockRetriever)
	r.On("Retrieve", mock.Anything, mock.Anything).Return(nil, errors.New("db error"))
	uc := NewSearchUsecase(r)
	uid := "user-1"
	out := uc.Search(context.Background(), SearchInput{Query: "test", UserID: &uid, Limit: 10})
	assert.Equal(t, []SearchResultItem{}, out.Results)
	r.AssertExpectations(t)
}

func TestSearchUsecase_HappyPath_MapsResults(t *testing.T) {
	session := "s1"
	now := time.Now()
	mems := []retrieval.RetrievedMemory{
		{ScoredMemory: store.ScoredMemory{
			Memory: store.Memory{Value: "happy", SourceSession: &session, CreatedAt: now},
			Score:  0.85,
		}},
	}
	r := new(MockRetriever)
	r.On("Retrieve", mock.Anything, mock.Anything).Return(mems, nil)
	uc := NewSearchUsecase(r)
	uid := "user-1"
	out := uc.Search(context.Background(), SearchInput{Query: "mood", UserID: &uid, Limit: 5})
	assert.Len(t, out.Results, 1)
	assert.Equal(t, "happy", out.Results[0].Content)
	assert.InDelta(t, 0.85, out.Results[0].Score, 0.01)
	assert.Equal(t, "s1", out.Results[0].SessionID)
	r.AssertExpectations(t)
}

func TestDerefString_Nil_ReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", derefString(nil))
}

func TestDerefString_NonNil_ReturnsValue(t *testing.T) {
	s := "hello"
	assert.Equal(t, "hello", derefString(&s))
}
