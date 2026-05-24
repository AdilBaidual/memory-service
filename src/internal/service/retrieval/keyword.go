// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"fmt"

	"memory-service/internal/adapters/store"
	"memory-service/internal/identity"
)

// keywordSearch runs a Postgres full-text search against the value_tsv generated
// column using the 'english' configuration. Builds an OR tsquery from the query's
// lexemes so that natural-language questions ("What does the user think about X?")
// match documents containing any key term rather than requiring all of them.
// Returns up to limit results ordered by ts_rank_cd descending. Returns empty
// slice (not error) when the query produces no FTS tokens.
func keywordSearch(
	ctx context.Context,
	q store.Querier,
	scope identity.Scope,
	query string,
	limit int,
) ([]store.ScoredMemory, error) {
	// Build scope clause without table alias; memories is the only base table.
	scopeClause := store.ScopeWhere(scope, 1)
	sql := `
		WITH qt AS (
			SELECT string_agg(lexeme, ' | ') AS expr
			FROM unnest(to_tsvector('english', $2))
		)
		SELECT
			m.id, m.user_id, m.type, m.key, m.value, m.evidence, m.confidence,
			m.entities, m.source_session, m.source_turn, m.supersedes, m.active,
			m.valid_from, m.valid_to, m.created_at, m.updated_at, m.metadata,
			ts_rank_cd(m.value_tsv, to_tsquery('english', qt.expr)) AS score
		FROM memories m, qt
		WHERE ` + scopeClause + `
		  AND m.active  = true
		  AND qt.expr   IS NOT NULL
		  AND m.value_tsv @@ to_tsquery('english', qt.expr)
		ORDER BY score DESC
		LIMIT $3`

	rows, err := q.Query(ctx, sql, scope.Value, query, limit)
	if err != nil {
		return nil, fmt.Errorf("keyword search query: %w", err)
	}
	defer rows.Close()

	var results []store.ScoredMemory
	for rows.Next() {
		var m store.ScoredMemory
		if err := scanScoredMemory(rows, &m); err != nil {
			return nil, fmt.Errorf("scan keyword row: %w", err)
		}
		results = append(results, m)
	}
	return results, rows.Err()
}
