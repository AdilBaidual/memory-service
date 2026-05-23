// Package storage handles database connectivity, schema migrations, and data access.
package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	pgvector "github.com/pgvector/pgvector-go"
)

// Memory represents a structured knowledge memory row.
type Memory struct {
	ID            uuid.UUID
	UserID        string
	Type          string
	Key           *string
	Value         string
	Evidence      string
	Confidence    float32
	Entities      json.RawMessage
	ValidFrom     *time.Time
	ValidTo       *time.Time
	Supersedes    *uuid.UUID
	Active        bool
	SourceSession *string
	SourceTurn    *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Metadata      json.RawMessage
}

// ScoredMemory is a Memory with a retrieval score attached.
type ScoredMemory struct {
	Memory
	Score float32
}

// InsertMemoryParams holds the parameters for InsertMemory.
type InsertMemoryParams struct {
	UserID        string
	Type          string
	Key           *string
	Value         string
	Evidence      string
	Confidence    float32
	Entities      json.RawMessage // []string serialized, default "[]"
	Embedding     []float32       // nil means store NULL in DB
	SourceSession *string
	SourceTurn    *uuid.UUID
}

// ListMemoriesFilters controls which memories are returned.
type ListMemoriesFilters struct {
	Type   *string
	Active *bool
	Key    *string
	Limit  int
}

// InsertMemory inserts a single memory row and returns the generated ID.
func InsertMemory(ctx context.Context, q Querier, m InsertMemoryParams) (uuid.UUID, error) {
	entities := m.Entities
	if len(entities) == 0 {
		entities = json.RawMessage("[]")
	}

	var embeddingArg any
	if m.Embedding != nil {
		embeddingArg = pgvector.NewVector(m.Embedding)
	}

	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO memories (
			user_id, type, key, value, evidence, confidence, entities,
			embedding, source_session, source_turn, active,
			created_at, updated_at, metadata
		) VALUES (
			$1, $2::memory_type, $3, $4, $5, $6, $7,
			$8, $9, $10, true,
			NOW(), NOW(), '{}'
		) RETURNING id
	`,
		m.UserID, m.Type, m.Key, m.Value, m.Evidence, m.Confidence, []byte(entities),
		embeddingArg, m.SourceSession, m.SourceTurn,
	).Scan(&id)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("insert memory: %w", err)
	}
	return id, nil
}

// GetTopKByCosine returns the top-k most similar active memories for a user.
func GetTopKByCosine(ctx context.Context, q Querier, userID string, queryEmbedding []float32, k int) ([]ScoredMemory, error) {
	rows, err := q.Query(ctx, `
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, source_session, source_turn, supersedes, active,
			valid_from, valid_to, created_at, updated_at, metadata,
			1 - (embedding <=> $2) AS score
		FROM memories
		WHERE user_id = $1
		  AND active = true
		  AND embedding IS NOT NULL
		ORDER BY embedding <=> $2
		LIMIT $3
	`, userID, pgvector.NewVector(queryEmbedding), k)
	if err != nil {
		return nil, fmt.Errorf("cosine search: %w", err)
	}
	defer rows.Close()

	var memories []ScoredMemory
	for rows.Next() {
		var m ScoredMemory
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
			&m.Confidence, &m.Entities, &m.SourceSession, &m.SourceTurn,
			&m.Supersedes, &m.Active, &m.ValidFrom, &m.ValidTo,
			&m.CreatedAt, &m.UpdatedAt, &m.Metadata, &m.Score,
		); err != nil {
			return nil, fmt.Errorf("scan scored memory: %w", err)
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
		return nil, fmt.Errorf("iterate scored memories: %w", err)
	}
	return memories, nil
}

// GetCanonicalKeys returns distinct non-null keys for active fact and preference memories.
func GetCanonicalKeys(ctx context.Context, q Querier, userID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT key
		FROM memories
		WHERE user_id = $1
		  AND active = true
		  AND key IS NOT NULL
		  AND type IN ('fact', 'preference')
		ORDER BY key
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get canonical keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan canonical key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// GetOpinionTopics returns distinct keys for opinion memories for a user.
func GetOpinionTopics(ctx context.Context, q Querier, userID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT key
		FROM memories
		WHERE user_id = $1
		  AND key IS NOT NULL
		  AND type IN ('opinion', 'opinion_view')
		ORDER BY key
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get opinion topics: %w", err)
	}
	defer rows.Close()

	var topics []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan opinion topic: %w", err)
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
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
