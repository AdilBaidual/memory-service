package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	pgvector "github.com/pgvector/pgvector-go"
)

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

type ScoredMemory struct {
	Memory
	Score float32
}

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

// Used by the consolidation UPDATE path.
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
