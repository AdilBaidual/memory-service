// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockSessionDeleter struct {
	mem, turns int64
	err        error
	called     bool
	calledWith string
}

func (m *mockSessionDeleter) DeleteSession(_ context.Context, sessionID string) (int64, int64, error) {
	m.called = true
	m.calledWith = sessionID
	return m.mem, m.turns, m.err
}

func TestDeleteSessionUsecase_HappyPath(t *testing.T) {
	d := &mockSessionDeleter{mem: 3, turns: 1}
	uc := NewDeleteSessionUsecase(d)
	res, err := uc.Delete(context.Background(), "session-1")
	assert.NoError(t, err)
	assert.Equal(t, int64(3), res.MemoriesDeleted)
	assert.Equal(t, int64(1), res.TurnsDeleted)
	assert.True(t, d.called)
	assert.Equal(t, "session-1", d.calledWith)
}

func TestDeleteSessionUsecase_DeleterError_Propagated(t *testing.T) {
	d := &mockSessionDeleter{err: errors.New("db error")}
	uc := NewDeleteSessionUsecase(d)
	_, err := uc.Delete(context.Background(), "session-1")
	assert.ErrorContains(t, err, "delete session")
}
