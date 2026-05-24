package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	pgvector "github.com/pgvector/pgvector-go"
)

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
