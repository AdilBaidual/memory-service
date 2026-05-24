// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockSessionDeleter mocks SessionDeleter using testify/mock.
type MockSessionDeleter struct{ mock.Mock }

func (m *MockSessionDeleter) DeleteSession(ctx context.Context, sessionID string) (int64, int64, error) {
	args := m.Called(ctx, sessionID)
	return args.Get(0).(int64), args.Get(1).(int64), args.Error(2)
}

func TestDeleteSessionUsecase_HappyPath(t *testing.T) {
	d := new(MockSessionDeleter)
	d.On("DeleteSession", mock.Anything, "session-1").Return(int64(3), int64(1), nil)
	uc := NewDeleteSessionUsecase(d)
	res, err := uc.Delete(context.Background(), "session-1")
	assert.NoError(t, err)
	assert.Equal(t, int64(3), res.MemoriesDeleted)
	assert.Equal(t, int64(1), res.TurnsDeleted)
	d.AssertExpectations(t)
}

func TestDeleteSessionUsecase_DeleterError_Propagated(t *testing.T) {
	d := new(MockSessionDeleter)
	d.On("DeleteSession", mock.Anything, "session-1").Return(int64(0), int64(0), errors.New("db error"))
	uc := NewDeleteSessionUsecase(d)
	_, err := uc.Delete(context.Background(), "session-1")
	assert.ErrorContains(t, err, "delete session")
	d.AssertExpectations(t)
}
