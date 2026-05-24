// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"fmt"
)

// UserDeleter abstracts the storage layer for deleting a user and all their data.
type UserDeleter interface {
	DeleteUser(ctx context.Context, userID string) error
}

type DeleteUserUsecase struct {
	deleter UserDeleter
}

func NewDeleteUserUsecase(d UserDeleter) *DeleteUserUsecase {
	return &DeleteUserUsecase{deleter: d}
}

func (uc *DeleteUserUsecase) Delete(ctx context.Context, userID string) error {
	if err := uc.deleter.DeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}
