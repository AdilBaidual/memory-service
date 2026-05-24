// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockUserDeleter mocks UserDeleter using testify/mock.
type MockUserDeleter struct{ mock.Mock }

func (m *MockUserDeleter) DeleteUser(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func TestDeleteUserUsecase_HappyPath(t *testing.T) {
	d := new(MockUserDeleter)
	d.On("DeleteUser", mock.Anything, "user-1").Return(nil)
	uc := NewDeleteUserUsecase(d)
	err := uc.Delete(context.Background(), "user-1")
	assert.NoError(t, err)
	d.AssertExpectations(t)
}

func TestDeleteUserUsecase_DeleterError_Propagated(t *testing.T) {
	d := new(MockUserDeleter)
	d.On("DeleteUser", mock.Anything, "user-1").Return(errors.New("db error"))
	uc := NewDeleteUserUsecase(d)
	err := uc.Delete(context.Background(), "user-1")
	assert.ErrorContains(t, err, "delete user")
	d.AssertExpectations(t)
}
