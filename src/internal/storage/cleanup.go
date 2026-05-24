// Package storage handles database connectivity, schema migrations, and data access.
package storage

import (
	"context"
	"fmt"
)

// DeleteSessionData removes all data originating from a session: memories
// (entity_mentions and entity_relationships cascade via FK) and turns.
// Returns counts of deleted memories and turns. Idempotent.
// Must be called within a transaction (caller manages commit/rollback).
func DeleteSessionData(ctx context.Context, q Querier, sessionID string) (memoriesDeleted, turnsDeleted int64, err error) {
	tag, err := q.Exec(ctx, "DELETE FROM memories WHERE source_session = $1", sessionID)
	if err != nil {
		return 0, 0, fmt.Errorf("delete memories by session: %w", err)
	}
	memoriesDeleted = tag.RowsAffected()

	tag, err = q.Exec(ctx, "DELETE FROM turns WHERE session_id = $1", sessionID)
	if err != nil {
		return 0, 0, fmt.Errorf("delete turns by session: %w", err)
	}
	turnsDeleted = tag.RowsAffected()

	return memoriesDeleted, turnsDeleted, nil
}

// DeleteAllUserData removes all rows for a user across all tables in FK-safe order.
// Must be called within a transaction (caller manages commit/rollback).
func DeleteAllUserData(ctx context.Context, q Querier, userID string) error {
	steps := []struct {
		table string
		sql   string
	}{
		{"entity_relationships", "DELETE FROM entity_relationships WHERE user_id = $1"},
		{"entity_mentions", "DELETE FROM entity_mentions WHERE user_id = $1"},
		{"entities", "DELETE FROM entities WHERE user_id = $1"},
		{"memories", "DELETE FROM memories WHERE user_id = $1"},
		{"turns", "DELETE FROM turns WHERE user_id = $1"},
	}

	for _, s := range steps {
		if _, err := q.Exec(ctx, s.sql, userID); err != nil {
			return fmt.Errorf("delete %s: %w", s.table, err)
		}
	}
	return nil
}
