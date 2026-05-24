// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/adapters/store"
)

// DeleteUserUsecase handles user deletion.
type DeleteUserUsecase struct {
	pool *pgxpool.Pool
}

func NewDeleteUserUsecase(pool *pgxpool.Pool) *DeleteUserUsecase {
	return &DeleteUserUsecase{pool: pool}
}

// Delete removes all data for a user across all tables.
func (uc *DeleteUserUsecase) Delete(ctx context.Context, userID string) error {
	tx, err := uc.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := store.DeleteAllUserData(ctx, tx, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
