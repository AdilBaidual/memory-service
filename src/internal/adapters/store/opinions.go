// Package store provides PostgreSQL-backed implementations of usecase storage interfaces.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	pgvector "github.com/pgvector/pgvector-go"

	"memory-service/internal/identity"
)

// OpinionViewWithEmbedding pairs an opinion_view Memory with its stored embedding
// for cosine-similarity filtering in the recall pipeline.
type OpinionViewWithEmbedding struct {
	Memory
	Embedding []float32 // nil when no embedding was stored
}

// GetRawOpinionsByKey returns all active raw opinions for the given scope+key,
// ordered by created_at ASC (chronological order for synthesis prompts).
func GetRawOpinionsByKey(ctx context.Context, q Querier, scope identity.Scope, key string) ([]Memory, error) {
	rows, err := q.Query(ctx, `
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, valid_from, valid_to, supersedes, active,
			source_session, source_turn, created_at, updated_at, metadata
		FROM memories
		WHERE `+ScopeWhere(scope, 1)+`
		  AND type    = 'opinion'::memory_type
		  AND key     = $2
		  AND active  = true
		ORDER BY created_at ASC
	`, scope.Value, key)
	if err != nil {
		return nil, fmt.Errorf("get raw opinions by key: %w", err)
	}
	defer rows.Close()

	var mems []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
			&m.Confidence, &m.Entities, &m.ValidFrom, &m.ValidTo,
			&m.Supersedes, &m.Active, &m.SourceSession, &m.SourceTurn,
			&m.CreatedAt, &m.UpdatedAt, &m.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan raw opinion: %w", err)
		}
		if m.Entities == nil {
			m.Entities = json.RawMessage("[]")
		}
		if m.Metadata == nil {
			m.Metadata = json.RawMessage("{}")
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// UpsertOpinionView inserts a new opinion_view memory, superseding the previous
// one for the same (scope, key) if it exists. Caller wraps in a transaction
// for atomicity of the supersede+insert pair. Pass a non-nil embedding to
// enable cosine-similarity filtering on future recall queries.
func UpsertOpinionView(ctx context.Context, q Querier, scope identity.Scope, key, value string, sourceSession *string, embedding []float32) error {
	existing, err := FindActiveByKeyScoped(ctx, q, scope, "opinion_view", key)
	if err != nil {
		return fmt.Errorf("find existing opinion_view: %w", err)
	}

	var supersedes *uuid.UUID
	if existing != nil {
		if err := MarkSuperseded(ctx, q, existing.ID); err != nil {
			return fmt.Errorf("mark existing opinion_view superseded: %w", err)
		}
		id := existing.ID
		supersedes = &id
	}

	_, err = InsertMemory(ctx, q, InsertMemoryParams{
		Scope:         scope,
		Type:          "opinion_view",
		Key:           &key,
		Value:         value,
		Evidence:      "synthesized",
		Confidence:    0.9,
		Entities:      json.RawMessage("[]"),
		Embedding:     embedding,
		SourceSession: sourceSession,
		Supersedes:    supersedes,
	})
	if err != nil {
		return fmt.Errorf("insert opinion_view: %w", err)
	}
	return nil
}

// GetActiveOpinionViewsWithEmbeddings returns all active opinion_view memories
// for the given scope along with their stored embeddings. Used by the recall pipeline
// to filter opinion views by cosine similarity to the current query.
func GetActiveOpinionViewsWithEmbeddings(ctx context.Context, q Querier, scope identity.Scope) ([]OpinionViewWithEmbedding, error) {
	rows, err := q.Query(ctx, `
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, valid_from, valid_to, supersedes, active,
			source_session, source_turn, created_at, updated_at, metadata,
			embedding
		FROM memories
		WHERE `+ScopeWhere(scope, 1)+`
		  AND active  = true
		  AND type    = 'opinion_view'::memory_type
		ORDER BY confidence DESC, updated_at DESC
	`, scope.Value)
	if err != nil {
		return nil, fmt.Errorf("get opinion views with embeddings: %w", err)
	}
	defer rows.Close()

	var results []OpinionViewWithEmbedding
	for rows.Next() {
		var m Memory
		var embVec *pgvector.Vector
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
			&m.Confidence, &m.Entities, &m.ValidFrom, &m.ValidTo,
			&m.Supersedes, &m.Active, &m.SourceSession, &m.SourceTurn,
			&m.CreatedAt, &m.UpdatedAt, &m.Metadata,
			&embVec,
		); err != nil {
			return nil, fmt.Errorf("scan opinion view: %w", err)
		}
		if m.Entities == nil {
			m.Entities = json.RawMessage("[]")
		}
		if m.Metadata == nil {
			m.Metadata = json.RawMessage("{}")
		}
		ovm := OpinionViewWithEmbedding{Memory: m}
		if embVec != nil {
			ovm.Embedding = embVec.Slice()
		}
		results = append(results, ovm)
	}
	return results, rows.Err()
}

// GetActiveMemoriesByTypes returns all active memories of given types for the given scope,
// sorted by confidence DESC, updated_at DESC. Returns nil when types is empty.
func GetActiveMemoriesByTypes(ctx context.Context, q Querier, scope identity.Scope, types []string) ([]Memory, error) {
	if len(types) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(types)+1)
	args = append(args, scope.Value)
	placeholders := make([]string, len(types))
	for i, t := range types {
		args = append(args, t)
		placeholders[i] = fmt.Sprintf("$%d::memory_type", i+2)
	}

	query := fmt.Sprintf(`
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, valid_from, valid_to, supersedes, active,
			source_session, source_turn, created_at, updated_at, metadata
		FROM memories
		WHERE %s
		  AND active  = true
		  AND type IN (%s)
		ORDER BY confidence DESC, updated_at DESC
	`, ScopeWhere(scope, 1), strings.Join(placeholders, ", "))

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get active memories by types: %w", err)
	}
	defer rows.Close()

	var mems []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
			&m.Confidence, &m.Entities, &m.ValidFrom, &m.ValidTo,
			&m.Supersedes, &m.Active, &m.SourceSession, &m.SourceTurn,
			&m.CreatedAt, &m.UpdatedAt, &m.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan memory by type: %w", err)
		}
		if m.Entities == nil {
			m.Entities = json.RawMessage("[]")
		}
		if m.Metadata == nil {
			m.Metadata = json.RawMessage("{}")
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}

// GetMemoriesBySession returns active memories sourced from a specific session,
// ordered by created_at DESC.
func GetMemoriesBySession(ctx context.Context, q Querier, sessionID string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := q.Query(ctx, `
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, valid_from, valid_to, supersedes, active,
			source_session, source_turn, created_at, updated_at, metadata
		FROM memories
		WHERE source_session = $1
		  AND active         = true
		ORDER BY created_at DESC
		LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("get memories by session: %w", err)
	}
	defer rows.Close()

	var mems []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(
			&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
			&m.Confidence, &m.Entities, &m.ValidFrom, &m.ValidTo,
			&m.Supersedes, &m.Active, &m.SourceSession, &m.SourceTurn,
			&m.CreatedAt, &m.UpdatedAt, &m.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scan memory by session: %w", err)
		}
		if m.Entities == nil {
			m.Entities = json.RawMessage("[]")
		}
		if m.Metadata == nil {
			m.Metadata = json.RawMessage("{}")
		}
		mems = append(mems, m)
	}
	return mems, rows.Err()
}
