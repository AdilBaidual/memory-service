// Package store provides PostgreSQL-backed implementations of usecase storage interfaces.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/identity"
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

// CountMemoriesByUser returns the total count of matching memories (ignoring limit/offset).
func (l *PoolMemoryLister) CountMemoriesByUser(ctx context.Context, userID string, f ListMemoriesFilters) (int, error) {
	return CountMemoriesByUser(ctx, l.pool, userID, f)
}

// PoolStableMemoryLoader implements usecase.StableMemoryLoader using a pool.
type PoolStableMemoryLoader struct {
	pool *pgxpool.Pool
}

// NewPoolStableMemoryLoader constructs a PoolStableMemoryLoader.
func NewPoolStableMemoryLoader(pool *pgxpool.Pool) *PoolStableMemoryLoader {
	return &PoolStableMemoryLoader{pool: pool}
}

// GetActiveMemoriesByTypes returns active memories of the given types for the given scope.
func (s *PoolStableMemoryLoader) GetActiveMemoriesByTypes(ctx context.Context, scope identity.Scope, types []string) ([]Memory, error) {
	return GetActiveMemoriesByTypes(ctx, s.pool, scope, types)
}

// GetActiveOpinionViewsWithEmbeddings returns active opinion_view memories with their embeddings.
func (s *PoolStableMemoryLoader) GetActiveOpinionViewsWithEmbeddings(ctx context.Context, scope identity.Scope) ([]OpinionViewWithEmbedding, error) {
	return GetActiveOpinionViewsWithEmbeddings(ctx, s.pool, scope)
}

// PoolSessionQuerier implements usecase.SessionQuerier using a pool.
type PoolSessionQuerier struct {
	pool *pgxpool.Pool
}

// NewPoolSessionQuerier constructs a PoolSessionQuerier.
func NewPoolSessionQuerier(pool *pgxpool.Pool) *PoolSessionQuerier {
	return &PoolSessionQuerier{pool: pool}
}

// GetMemoriesBySession returns active memories from a specific session.
func (s *PoolSessionQuerier) GetMemoriesBySession(ctx context.Context, sessionID string, limit int) ([]Memory, error) {
	return GetMemoriesBySession(ctx, s.pool, sessionID, limit)
}
