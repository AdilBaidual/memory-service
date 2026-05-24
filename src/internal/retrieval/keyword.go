// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"

	"memory-service/internal/storage"
)

// keywordSearch runs a Postgres full-text search against the value_tsv generated
// column using the 'english' configuration. Returns up to limit results ordered
// by ts_rank_cd descending. Returns empty slice (not error) when query produces
// no FTS tokens.
func keywordSearch(
	ctx context.Context,
	q storage.Querier,
	userID string,
	query string,
	limit int,
) ([]storage.ScoredMemory, error) {
	const sql = `
		SELECT
			id, user_id, type, key, value, evidence, confidence,
			entities, source_session, source_turn, supersedes, active,
			valid_from, valid_to, created_at, updated_at, metadata,
			ts_rank_cd(value_tsv, plainto_tsquery('english', $2)) AS score
		FROM memories
		WHERE user_id = $1
		  AND active  = true
		  AND value_tsv @@ plainto_tsquery('english', $2)
		ORDER BY score DESC
		LIMIT $3`

	rows, err := q.Query(ctx, sql, userID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search query: %w", err)
	}
	defer rows.Close()

	var results []storage.ScoredMemory
	for rows.Next() {
		var m storage.ScoredMemory
		if err := scanScoredMemory(rows, &m); err != nil {
			return nil, fmt.Errorf("scan keyword row: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}
