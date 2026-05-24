// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/adapters/store"
)

// DeleteSessionResult holds the counts of rows removed.
type DeleteSessionResult struct {
	MemoriesDeleted int64
	TurnsDeleted    int64
}

// DeleteSessionUsecase handles session deletion.
type DeleteSessionUsecase struct {
	pool *pgxpool.Pool
}

func NewDeleteSessionUsecase(pool *pgxpool.Pool) *DeleteSessionUsecase {
	return &DeleteSessionUsecase{pool: pool}
}

// Delete removes all data for a session and returns deletion counts.
func (uc *DeleteSessionUsecase) Delete(ctx context.Context, sessionID string) (DeleteSessionResult, error) {
	tx, err := uc.pool.Begin(ctx)
	if err != nil {
		return DeleteSessionResult{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	memoriesDeleted, turnsDeleted, err := store.DeleteSessionData(ctx, tx, sessionID)
	if err != nil {
		return DeleteSessionResult{}, fmt.Errorf("delete session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return DeleteSessionResult{}, fmt.Errorf("commit transaction: %w", err)
	}

	return DeleteSessionResult{MemoriesDeleted: memoriesDeleted, TurnsDeleted: turnsDeleted}, nil
}
