// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"memory-service/internal/storage"
)

// scanScoredMemory scans a single row from a ScoredMemory query result.
// Column order must be:
//
//	id, user_id, type, key, value, evidence, confidence,
//	entities, source_session, source_turn, supersedes, active,
//	valid_from, valid_to, created_at, updated_at, metadata, score
func scanScoredMemory(rows pgx.Rows, m *storage.ScoredMemory) error {
	if err := rows.Scan(
		&m.ID, &m.UserID, &m.Type, &m.Key, &m.Value, &m.Evidence,
		&m.Confidence, &m.Entities, &m.SourceSession, &m.SourceTurn,
		&m.Supersedes, &m.Active, &m.ValidFrom, &m.ValidTo,
		&m.CreatedAt, &m.UpdatedAt, &m.Metadata, &m.Score,
	); err != nil {
		return err
	}
	if m.Entities == nil {
		m.Entities = json.RawMessage("[]")
	}
	if m.Metadata == nil {
		m.Metadata = json.RawMessage("{}")
	}
	return nil
}
