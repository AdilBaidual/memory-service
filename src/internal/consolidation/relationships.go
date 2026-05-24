// Package consolidation handles ADD/UPDATE/NOOP decisions for extracted memories,
// maintaining bi-temporal supersedes chains.
package consolidation

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"memory-service/internal/llm"
	"memory-service/internal/storage"
)

// ProcessRelationships persists entity triplets from a turn.
// For each relationship:
//  1. Upsert subject and object as entities
//  2. Insert the relationship (append-only)
//  3. Link entities to source memory via entity_mentions
//
// Non-fatal: errors are logged but do not abort the transaction.
// Relationships are derived navigation data; missing ones don't
// break correctness — retrieval degrades gracefully to semantic+FTS.
//
// No-ops silently when sourceMemoryID is uuid.Nil (no memory to link to).
func ProcessRelationships(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	relationships []llm.Relationship,
	sourceMemoryID uuid.UUID,
) error {
	if sourceMemoryID == uuid.Nil {
		return nil
	}

	for _, rel := range relationships {
		subj := strings.TrimSpace(rel.Subject)
		pred := strings.TrimSpace(rel.Predicate)
		obj := strings.TrimSpace(rel.Object)

		if subj == "" || pred == "" || obj == "" {
			continue
		}

		if err := storage.UpsertEntity(ctx, tx, subj, userID,
			inferEntityType(subj)); err != nil {
			return fmt.Errorf("upsert subject %q: %w", subj, err)
		}

		if err := storage.UpsertEntity(ctx, tx, obj, userID,
			inferEntityType(obj)); err != nil {
			return fmt.Errorf("upsert object %q: %w", obj, err)
		}

		if err := storage.InsertRelationship(ctx, tx,
			userID, subj, pred, obj, sourceMemoryID); err != nil {
			return fmt.Errorf("insert relationship (%s,%s,%s): %w",
				subj, pred, obj, err)
		}

		if err := storage.InsertEntityMention(ctx, tx,
			sourceMemoryID, subj, userID, "subject"); err != nil {
			return fmt.Errorf("mention subject %q: %w", subj, err)
		}
		if err := storage.InsertEntityMention(ctx, tx,
			sourceMemoryID, obj, userID, "object"); err != nil {
			return fmt.Errorf("mention object %q: %w", obj, err)
		}
	}
	return nil
}

// inferEntityType guesses entity type from name.
// Heuristic only — entity_type is nullable, this is best-effort.
func inferEntityType(name string) string {
	if strings.ToLower(name) == "user" {
		return "person"
	}
	return ""
}
