// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"fmt"
)

// SessionDeleter abstracts the storage layer for deleting a session.
type SessionDeleter interface {
	DeleteSession(ctx context.Context, sessionID string) (memoriesDeleted, turnsDeleted int64, err error)
}

type DeleteSessionResult struct {
	MemoriesDeleted int64
	TurnsDeleted    int64
}

type DeleteSessionUsecase struct {
	deleter SessionDeleter
}

func NewDeleteSessionUsecase(d SessionDeleter) *DeleteSessionUsecase {
	return &DeleteSessionUsecase{deleter: d}
}

func (uc *DeleteSessionUsecase) Delete(ctx context.Context, sessionID string) (DeleteSessionResult, error) {
	mem, turns, err := uc.deleter.DeleteSession(ctx, sessionID)
	if err != nil {
		return DeleteSessionResult{}, fmt.Errorf("delete session: %w", err)
	}
	return DeleteSessionResult{MemoriesDeleted: mem, TurnsDeleted: turns}, nil
}
