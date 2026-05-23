// Package storage handles database connectivity, schema migrations, and data access.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Memory represents a structured knowledge memory row.
type Memory struct {
	ID            string
	UserID        string
	Type          string
	Key           *string
	Value         string
	Evidence      string
	Confidence    float32
	Entities      json.RawMessage
	ValidFrom     *time.Time
	ValidTo       *time.Time
	Supersedes    *string
	Active        bool
	SourceSession *string
	SourceTurn    *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Metadata      json.RawMessage
}

// ListMemoriesFilters controls which memories are returned.
// A nil pointer means "no filter on this field".
type ListMemoriesFilters struct {
	Type   *string
	Active *bool // nil = all; true = active only; false = inactive only
	Key    *string
	Limit  int // default 100 when 0; hard cap 1000
}

// ListMemoriesByUser returns memories for a user ordered by active DESC, type, key, created_at DESC.
// The embedding and value_tsv columns are excluded.
func ListMemoriesByUser(ctx context.Context, q Querier, userID string, f ListMemoriesFilters) ([]Memory, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	base := `
		SELECT id, user_id, type, key, value, evidence, confidence, entities,
		       valid_from, valid_to, supersedes, active, source_session,
		       source_turn, created_at, updated_at, metadata
		FROM memories
		WHERE user_id = $1`

	args := []any{userID}
	argIdx := 2
	var extra []string

	if f.Type != nil {
		extra = append(extra, fmt.Sprintf("type = $%d::memory_type", argIdx))
		args = append(args, *f.Type)
		argIdx++
	}
	if f.Active != nil {
		extra = append(extra, fmt.Sprintf("active = $%d", argIdx))
		args = append(args, *f.Active)
		argIdx++
	}
	if f.Key != nil {
		extra = append(extra, fmt.Sprintf("key = $%d", argIdx))
		args = append(args, *f.Key)
		argIdx++
	}

	query := base
	if len(extra) > 0 {
		query += " AND " + strings.Join(extra, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY active DESC, type, key, created_at DESC LIMIT $%d", argIdx)
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
