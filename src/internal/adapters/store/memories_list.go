package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// argBase is the $N index of the first filter argument (typically 2, since $1 = userID).
func buildListFilters(f ListMemoriesFilters, argBase int) (clauses []string, args []any) {
	i := argBase
	if f.Type != nil {
		clauses = append(clauses, fmt.Sprintf("type = $%d::memory_type", i))
		args = append(args, *f.Type)
		i++
	}
	if f.Active != nil {
		clauses = append(clauses, fmt.Sprintf("active = $%d", i))
		args = append(args, *f.Active)
		i++
	}
	if f.Key != nil {
		clauses = append(clauses, fmt.Sprintf("key = $%d", i))
		args = append(args, *f.Key)
		i++ //nolint:ineffassign // keeps argBase incrementing correctly if more filters are added
	}
	return
}

// ListMemoriesByUser returns memories for a user ordered by active DESC, type, key, created_at DESC.
func ListMemoriesByUser(ctx context.Context, q Querier, userID string, f ListMemoriesFilters) ([]Memory, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	const base = `
		SELECT id, user_id, type, key, value, evidence, confidence, entities,
		       valid_from, valid_to, supersedes, active, source_session,
		       source_turn, created_at, updated_at, metadata
		FROM memories
		WHERE user_id = $1`

	filterClauses, filterArgs := buildListFilters(f, 2)
	args := append([]any{userID}, filterArgs...)

	query := base
	if len(filterClauses) > 0 {
		query += " AND " + strings.Join(filterClauses, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY active DESC, type, key, created_at DESC LIMIT $%d", len(args)+1)
	args = append(args, limit)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list memories by user: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
			&m.Confidence, &m.Entities, &m.ValidFrom, &m.ValidTo,
			&m.Supersedes, &m.Active, &m.SourceSession, &m.SourceTurn,
			&m.CreatedAt, &m.UpdatedAt, &m.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan memory: %w", err)
		}
		if m.Entities == nil {
			m.Entities = json.RawMessage("[]")
		}
		if m.Metadata == nil {
			m.Metadata = json.RawMessage("{}")
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}
	return memories, nil
}
