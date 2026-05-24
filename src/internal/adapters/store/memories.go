package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	Supersedes    *uuid.UUID // nil when this is not a superseding update
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
			embedding, source_session, source_turn, supersedes, active,
			created_at, updated_at, metadata
		) VALUES (
			$1, $2::memory_type, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, true,
			NOW(), NOW(), '{}'
		) RETURNING id
	`,
		m.UserID, m.Type, m.Key, m.Value, m.Evidence, m.Confidence, []byte(entities),
		embeddingArg, m.SourceSession, m.SourceTurn, m.Supersedes,
	).Scan(&id)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("insert memory: %w", err)
	}
	return id, nil
}

// GetTopKByCosine returns the top-k most similar active memories for a user.
func GetTopKByCosine(ctx context.Context, q Querier, userID string, queryEmbedding []float32, k int) ([]ScoredMemory, error) {
	if len(queryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding must be non-empty")
	}
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

// GetCanonicalKeyValues returns key+value pairs for active fact and preference memories.
// Returning the current value alongside the key lets the extraction LLM decide whether
// new information is the same type (reuse key) or a different type (new key).
func GetCanonicalKeyValues(ctx context.Context, q Querier, userID string) ([]struct{ Key, Value string }, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (key) key, value
		FROM memories
		WHERE user_id = $1
		  AND active = true
		  AND key IS NOT NULL
		  AND type IN ('fact', 'preference')
		ORDER BY key, updated_at DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get canonical key values: %w", err)
	}
	defer rows.Close()

	var pairs []struct{ Key, Value string }
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan canonical key value: %w", err)
		}
		pairs = append(pairs, struct{ Key, Value string }{k, v})
	}
	return pairs, rows.Err()
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

// FindActiveByKey returns the single active memory for a user with the given
// type and key, or nil if none exists.
// Returns (nil, nil) when no row is found — NOT an error.
func FindActiveByKey(ctx context.Context, q Querier, userID, memType, key string) (*Memory, error) {
	var m Memory
	err := q.QueryRow(ctx, `
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, valid_from, valid_to, supersedes, active,
			source_session, source_turn, created_at, updated_at, metadata
		FROM memories
		WHERE user_id = $1
		  AND type    = $2::memory_type
		  AND key     = $3
		  AND active  = true
		LIMIT 1
	`, userID, memType, key).Scan(
		&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
		&m.Confidence, &m.Entities, &m.ValidFrom, &m.ValidTo,
		&m.Supersedes, &m.Active, &m.SourceSession, &m.SourceTurn,
		&m.CreatedAt, &m.UpdatedAt, &m.Metadata,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active by key: %w", err)
	}
	if m.Entities == nil {
		m.Entities = json.RawMessage("[]")
	}
	if m.Metadata == nil {
		m.Metadata = json.RawMessage("{}")
	}
	return &m, nil
}

// MarkSuperseded marks a memory as inactive. Used by the consolidation UPDATE path.
func MarkSuperseded(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE memories
		SET active     = false,
		    valid_to   = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("mark superseded: %w", err)
	}
	return nil
}

// TouchMemory updates updated_at to NOW() without changing any other fields.
// Used by the consolidation NOOP path to signal "still current".
func TouchMemory(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `
		UPDATE memories
		SET updated_at = NOW()
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("touch memory: %w", err)
	}
	return nil
}

// buildListFilters returns WHERE clauses and args for ListMemoriesByUser filters.
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
