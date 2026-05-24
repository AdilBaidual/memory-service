// Package consolidation handles ADD/UPDATE/NOOP decisions for extracted memories,
// maintaining bi-temporal supersedes chains.
package consolidation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"memory-service/internal/storage"
)

// Result describes what consolidation did for a single candidate.
type Result int

const (
	ResultADD    Result = iota // new memory inserted
	ResultNOOP                 // same value already exists, touched updated_at
	ResultUPDATE               // contradicting fact superseded, new inserted
)

// String returns a human-readable label for the result.
func (r Result) String() string {
	switch r {
	case ResultADD:
		return "ADD"
	case ResultNOOP:
		return "NOOP"
	case ResultUPDATE:
		return "UPDATE"
	default:
		return "UNKNOWN"
	}
}

// ConsolidateFact applies ADD / NOOP / UPDATE logic for a single memory candidate.
// Must be called inside a transaction.
//
// When key is nil (events, keyless items): always ADD — nothing to consolidate against.
//
// Returns the ID of the inserted/existing memory and the result type.
func ConsolidateFact(
	ctx context.Context,
	tx pgx.Tx,
	userID string,
	memType string,
	key *string,
	value string,
	evidence string,
	confidence float32,
	entities []string,
	embedding []float32,
	sourceSession *string,
	sourceTurn *uuid.UUID,
) (uuid.UUID, Result, error) {
	if key == nil || *key == "" {
		id, err := insertNew(ctx, tx, userID, memType, key, value,
			evidence, confidence, entities, embedding,
			sourceSession, sourceTurn, nil)
		return id, ResultADD, err
	}

	existing, err := storage.FindActiveByKey(ctx, tx, userID, memType, *key)
	if err != nil {
		return uuid.Nil, ResultADD, fmt.Errorf("find active by key: %w", err)
	}

	if existing == nil {
		id, err := insertNew(ctx, tx, userID, memType, key, value,
			evidence, confidence, entities, embedding,
			sourceSession, sourceTurn, nil)
		return id, ResultADD, err
	}

	if normalizeValue(existing.Value) == normalizeValue(value) {
		if err := storage.TouchMemory(ctx, tx, existing.ID); err != nil {
			return existing.ID, ResultNOOP, fmt.Errorf("touch memory: %w", err)
		}
		return existing.ID, ResultNOOP, nil
	}

	if err := storage.MarkSuperseded(ctx, tx, existing.ID); err != nil {
		return uuid.Nil, ResultUPDATE, fmt.Errorf("mark superseded: %w", err)
	}
	id, err := insertNew(ctx, tx, userID, memType, key, value,
		evidence, confidence, entities, embedding,
		sourceSession, sourceTurn, &existing.ID)
	return id, ResultUPDATE, err
}

// normalizeValue lowercases and trims whitespace for comparison.
func normalizeValue(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func insertNew(
	ctx context.Context,
	tx pgx.Tx,
	userID, memType string,
	key *string,
	value, evidence string,
	confidence float32,
	entities []string,
	embedding []float32,
	sourceSession *string,
	sourceTurn *uuid.UUID,
	supersedes *uuid.UUID,
) (uuid.UUID, error) {
	entJSON, err := json.Marshal(entities)
	if err != nil {
		entJSON = []byte("[]")
	}

	id, err := storage.InsertMemory(ctx, tx, storage.InsertMemoryParams{
		UserID:        userID,
		Type:          memType,
		Key:           key,
		Value:         value,
		Evidence:      evidence,
		Confidence:    confidence,
		Entities:      json.RawMessage(entJSON),
		Embedding:     embedding,
		SourceSession: sourceSession,
		SourceTurn:    sourceTurn,
		Supersedes:    supersedes,
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert new memory: %w", err)
	}
	return id, nil
}
