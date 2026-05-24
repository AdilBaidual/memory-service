// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"
	"strings"

	"memory-service/internal/storage"
)

// graphSearch finds memories reachable from query entities via
// up to 2-hop entity_relationships traversal.
//
// Consistency guarantee: all graph queries filter through
// memories.active = true via JOIN. entity_relationships is
// append-only; stale edges are excluded naturally because their
// source_memory_id points to inactive memories.
//
// Hop 1 score: 1.0 (directly connected to query entity)
// Hop 2 score: 0.5 (one step removed)
func graphSearch(
	ctx context.Context,
	q storage.Querier,
	userID string,
	query string,
	limit int,
) ([]storage.ScoredMemory, error) {

	entities := extractQueryEntities(query)
	if len(entities) == 0 {
		return nil, nil
	}

	const sql = `
		WITH
		hop1_memories AS (
			SELECT DISTINCT m.id, 1.0::float4 AS hop_score
			FROM entity_mentions em
			JOIN memories m ON m.id = em.memory_id
			WHERE em.user_id     = $1
			  AND m.active       = true
			  AND em.entity_name = ANY($2::text[])
		),
		hop1_entities AS (
			SELECT DISTINCT er.object_entity AS name
			FROM entity_relationships er
			JOIN memories m ON m.id = er.source_memory_id
			WHERE er.user_id        = $1
			  AND er.subject_entity = ANY($2::text[])
			  AND m.active          = true
			UNION
			SELECT DISTINCT er.subject_entity
			FROM entity_relationships er
			JOIN memories m ON m.id = er.source_memory_id
			WHERE er.user_id       = $1
			  AND er.object_entity = ANY($2::text[])
			  AND m.active         = true
		),
		hop2_memories AS (
			SELECT DISTINCT m.id, 0.5::float4 AS hop_score
			FROM entity_mentions em
			JOIN memories m    ON m.id   = em.memory_id
			JOIN hop1_entities he ON he.name = em.entity_name
			WHERE em.user_id = $1
			  AND m.active   = true
		),
		all_memories AS (
			SELECT id, MAX(hop_score) AS score
			FROM (
				SELECT id, hop_score FROM hop1_memories
				UNION ALL
				SELECT id, hop_score FROM hop2_memories
			) combined
			GROUP BY id
		)
		SELECT
			m.id, m.user_id, m.type, m.key, m.value, m.evidence,
			m.confidence, m.entities, m.source_session, m.source_turn,
			m.supersedes, m.active, m.valid_from, m.valid_to,
			m.created_at, m.updated_at, m.metadata,
			am.score
		FROM all_memories am
		JOIN memories m ON m.id = am.id
		ORDER BY am.score DESC, m.created_at DESC
		LIMIT $3`

	rows, err := q.Query(ctx, sql, userID, entities, limit)
	if err != nil {
		return nil, fmt.Errorf("graph search query: %w", err)
	}
	defer rows.Close()

	var results []storage.ScoredMemory
	for rows.Next() {
		var m storage.ScoredMemory
		if err := scanScoredMemory(rows, &m); err != nil {
			return nil, fmt.Errorf("scan graph row: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// extractQueryEntities extracts candidate entity names from a query.
// Takes capitalized words that are not common stop words.
// v1: simple heuristic. v2: proper NER.
func extractQueryEntities(query string) []string {
	stopWords := map[string]bool{
		"What": true, "Where": true, "When": true, "Who": true,
		"How": true, "Why": true, "Does": true, "Do": true,
		"Is": true, "Are": true, "Was": true, "Were": true, "Did": true,
		"The": true, "A": true, "An": true, "In": true, "Of": true,
		"To": true, "For": true, "On": true, "With": true, "And": true,
		"Or": true, "But": true, "That": true, "This": true, "Their": true,
		"His": true, "Her": true, "Its": true, "My": true, "Your": true,
		"Our": true, "Has": true, "Have": true, "Had": true, "Can": true,
		"Could": true, "Would": true, "Should": true, "Will": true,
		"User": true, "Users": true, "Person": true, "People": true,
	}

	words := strings.Fields(query)
	seen := make(map[string]bool)
	var entities []string

	for _, word := range words {
		clean := strings.Trim(word, ".,?!;:'\"()")
		if len(clean) < 2 {
			continue
		}
		if clean[0] >= 'A' && clean[0] <= 'Z' && !stopWords[clean] {
			if !seen[clean] {
				seen[clean] = true
				entities = append(entities, clean)
			}
		}
	}
	return entities
}
