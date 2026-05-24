package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Increments mention_count on conflict.
func UpsertEntity(ctx context.Context, q Querier,
	name, userID, entityType string) error {

	_, err := q.Exec(ctx, `
		INSERT INTO entities
		    (name, user_id, canonical_name, entity_type,
		     first_mentioned_at, mention_count)
		VALUES ($1, $2, lower($1), $3, NOW(), 1)
		ON CONFLICT (name, user_id) DO UPDATE
		    SET mention_count = entities.mention_count + 1,
		        entity_type   = COALESCE(NULLIF(EXCLUDED.entity_type,''),
		                                 entities.entity_type)
		`, name, userID, entityType)
	if err != nil {
		return fmt.Errorf("upsert entity %q: %w", name, err)
	}
	return nil
}

// Idempotent via ON CONFLICT DO NOTHING.
func InsertEntityMention(ctx context.Context, q Querier,
	memoryID uuid.UUID, entityName, userID, role string) error {

	_, err := q.Exec(ctx, `
		INSERT INTO entity_mentions
		    (memory_id, entity_name, user_id, role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (memory_id, entity_name) DO NOTHING
		`, memoryID, entityName, userID, role)
	if err != nil {
		return fmt.Errorf("insert entity mention %q: %w", entityName, err)
	}
	return nil
}

// entity_relationships is append-only.
func InsertRelationship(ctx context.Context, q Querier,
	userID, subject, predicate, object string,
	sourceMemoryID uuid.UUID) error {

	_, err := q.Exec(ctx, `
		INSERT INTO entity_relationships
		    (user_id, subject_entity, predicate,
		     object_entity, source_memory_id, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		`, userID, subject, predicate, object, sourceMemoryID)
	if err != nil {
		return fmt.Errorf("insert relationship (%s,%s,%s): %w", subject, predicate, object, err)
	}
	return nil
}
