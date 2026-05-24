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
// entityMemoryMap maps lowercase entity names to their source memory IDs,
// allowing each relationship to be anchored to the most specific memory.
// The fallback ID is used when neither subject nor object appears in the map.
//
// Non-fatal: errors are logged but do not abort the transaction.
// Relationships are derived navigation data; missing ones don't
// break correctness — retrieval degrades gracefully to semantic+FTS.
func ProcessRelationships(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	relationships []llm.Relationship,
	entityMemoryMap map[string]uuid.UUID,
	fallbackMemoryID uuid.UUID,
) error {
	for _, rel := range relationships {
		// Normalize to lowercase so graph queries match regardless of how
		// the LLM capitalizes entity names (e.g. "user" vs "User").
		subj := strings.ToLower(strings.TrimSpace(rel.Subject))
		pred := strings.ToLower(strings.TrimSpace(rel.Predicate))
		obj := strings.ToLower(strings.TrimSpace(rel.Object))

		if subj == "" || pred == "" || obj == "" {
			continue
		}

		srcID := resolveSourceMemory(obj, subj, entityMemoryMap, fallbackMemoryID)
		if srcID == uuid.Nil {
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
			userID, subj, pred, obj, srcID); err != nil {
			return fmt.Errorf("insert relationship (%s,%s,%s): %w",
				subj, pred, obj, err)
		}

		if err := storage.InsertEntityMention(ctx, tx,
			srcID, subj, userID, "subject"); err != nil {
			return fmt.Errorf("mention subject %q: %w", subj, err)
		}
		if err := storage.InsertEntityMention(ctx, tx,
			srcID, obj, userID, "object"); err != nil {
			return fmt.Errorf("mention object %q: %w", obj, err)
		}
	}
	return nil
}

// resolveSourceMemory picks the memory that best anchors a relationship.
// Prefers the object entity (more specific), then subject, then fallback.
// "user" is skipped — it's too broad to be a useful anchor.
func resolveSourceMemory(obj, subj string, m map[string]uuid.UUID, fallback uuid.UUID) uuid.UUID {
	for _, name := range []string{obj, subj} {
		lower := strings.ToLower(name)
		if lower == "user" {
			continue
		}
		if id, ok := m[lower]; ok && id != uuid.Nil {
			return id
		}
	}
	return fallback
}

// inferEntityType guesses entity type from name.
// Heuristic only — entity_type is nullable, this is best-effort.
func inferEntityType(name string) string {
	if strings.ToLower(name) == "user" {
		return "person"
	}
	return ""
}
