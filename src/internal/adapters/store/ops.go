// Package store provides PostgreSQL-backed implementations of usecase storage interfaces.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolUserDeleter implements usecase.UserDeleter using a pool-managed transaction.
type PoolUserDeleter struct {
	pool *pgxpool.Pool
}

// NewPoolUserDeleter constructs a PoolUserDeleter.
func NewPoolUserDeleter(pool *pgxpool.Pool) *PoolUserDeleter {
	return &PoolUserDeleter{pool: pool}
}

// DeleteUser removes all rows for a user across all tables.
func (d *PoolUserDeleter) DeleteUser(ctx context.Context, userID string) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := DeleteAllUserData(ctx, tx, userID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// PoolSessionDeleter implements usecase.SessionDeleter using a pool-managed transaction.
type PoolSessionDeleter struct {
	pool *pgxpool.Pool
}

// NewPoolSessionDeleter constructs a PoolSessionDeleter.
func NewPoolSessionDeleter(pool *pgxpool.Pool) *PoolSessionDeleter {
	return &PoolSessionDeleter{pool: pool}
}

// DeleteSession removes all data originating from a session and returns deletion counts.
func (d *PoolSessionDeleter) DeleteSession(ctx context.Context, sessionID string) (int64, int64, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	mem, turns, err := DeleteSessionData(ctx, tx, sessionID)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, fmt.Errorf("commit transaction: %w", err)
	}
	return mem, turns, nil
}

// PoolMemoryLister implements usecase.MemoryLister using a pool.
type PoolMemoryLister struct {
	pool *pgxpool.Pool
}

// NewPoolMemoryLister constructs a PoolMemoryLister.
func NewPoolMemoryLister(pool *pgxpool.Pool) *PoolMemoryLister {
	return &PoolMemoryLister{pool: pool}
}

// ListMemoriesByUser returns memories for a user, applying the provided filters.
func (l *PoolMemoryLister) ListMemoriesByUser(ctx context.Context, userID string, f ListMemoriesFilters) ([]Memory, error) {
	return ListMemoriesByUser(ctx, l.pool, userID, f)
}
