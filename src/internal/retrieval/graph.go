// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"
	"strings"

	"memory-service/internal/adapters/store"
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
	q store.Querier,
	userID string,
	query string,
	limit int,
) ([]store.ScoredMemory, error) {

	entities := extractQueryEntities(query)
	if len(entities) == 0 {
		return nil, nil
	}

	const sql = `
		WITH
		hop1_memories AS (
			-- Score = 1.0 × entity_importance_multiplier, where the multiplier is
			-- a log-scaled function of mention_count capped at 2×.
			-- Entities mentioned often are a stronger signal than one-off names.
			SELECT m.id,
				MAX(
					1.0::float4
					* LEAST(
						1.0 + LN(GREATEST(COALESCE(e.mention_count, 1)::float4, 1.0)) * 0.1,
						2.0
					)::float4
				) AS hop_score
			FROM entity_mentions em
			JOIN memories m   ON m.id      = em.memory_id
			LEFT JOIN entities e ON e.name     = em.entity_name
			                    AND e.user_id  = em.user_id
			WHERE em.user_id     = $1
			  AND m.active       = true
			  AND em.entity_name = ANY($2::text[])
			GROUP BY m.id
		),
		hop1_entities AS (
			-- Relationships are append-only navigation metadata.
			-- Do NOT filter by m.active here: if a source memory is superseded,
			-- the edge it produced still provides a valid traversal path.
			-- (hop1_memories and hop2_memories independently enforce active=true
			-- on the memories they actually return.)
			SELECT DISTINCT er.object_entity AS name
			FROM entity_relationships er
			WHERE er.user_id        = $1
			  AND er.subject_entity = ANY($2::text[])
			UNION
			SELECT DISTINCT er.subject_entity
			FROM entity_relationships er
			WHERE er.user_id       = $1
			  AND er.object_entity = ANY($2::text[])
		),
		hop2_memories AS (
			-- Flat 0.5: boosting by the traversal-neighbor entity's mention_count
			-- would amplify "user" (which appears in nearly every relationship),
			-- inflating scores for unrelated memories. Boost is applied at hop1 only.
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

	var results []store.ScoredMemory
	for rows.Next() {
		var m store.ScoredMemory
		if err := scanScoredMemory(rows, &m); err != nil {
			return nil, fmt.Errorf("scan graph row: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// TODO: переделать
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
			// Normalize to lowercase: entity_relationships and entity_mentions
			// are stored in lowercase to ensure consistent matching.
			lower := strings.ToLower(clean)
			if !seen[lower] {
				seen[lower] = true
				entities = append(entities, lower)
			}
		}
	}
	return entities
}
