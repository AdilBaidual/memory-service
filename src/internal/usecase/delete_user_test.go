// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type mockUserDeleter struct {
	err        error
	called     bool
	calledWith string
}

func (m *mockUserDeleter) DeleteUser(_ context.Context, userID string) error {
	m.called = true
	m.calledWith = userID
	return m.err
}

func TestDeleteUserUsecase_HappyPath(t *testing.T) {
	d := &mockUserDeleter{}
	uc := NewDeleteUserUsecase(d)
	err := uc.Delete(context.Background(), "user-1")
	assert.NoError(t, err)
	assert.True(t, d.called)
	assert.Equal(t, "user-1", d.calledWith)
}

func TestDeleteUserUsecase_DeleterError_Propagated(t *testing.T) {
	d := &mockUserDeleter{err: errors.New("db error")}
	uc := NewDeleteUserUsecase(d)
	err := uc.Delete(context.Background(), "user-1")
	assert.ErrorContains(t, err, "delete user")
}
