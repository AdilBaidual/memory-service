package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TurnMessage is a single message from a turn, used for extraction.
type TurnMessage struct {
	Role    string
	Content string
}

// Turn represents a stored conversation turn.
type Turn struct {
	ID        uuid.UUID
	SessionID string
	UserID    *string
	Messages  json.RawMessage
	Timestamp time.Time
	Metadata  json.RawMessage
	CreatedAt time.Time
}

// InsertTurnParams holds the parameters for InsertTurn.
type InsertTurnParams struct {
	SessionID string
	UserID    *string
	Messages  json.RawMessage
	Timestamp time.Time
	Metadata  json.RawMessage
}

// InsertTurn writes a turn row and returns the generated UUID.
func InsertTurn(ctx context.Context, q Querier, p InsertTurnParams) (uuid.UUID, error) {
	metadata := p.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO turns (session_id, user_id, messages, timestamp, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, p.SessionID, p.UserID, []byte(p.Messages), p.Timestamp, []byte(metadata)).Scan(&id)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("insert turn: %w", err)
	}
	return id, nil
}

// DeleteTurnsBySession removes all turns for a session. Idempotent.
func DeleteTurnsBySession(ctx context.Context, q Querier, sessionID string) (int64, error) {
	tag, err := q.Exec(ctx, "DELETE FROM turns WHERE session_id = $1", sessionID)
	if err != nil {
		return 0, fmt.Errorf("delete turns by session: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteTurnsByUser removes all turns for a user. Idempotent.
func DeleteTurnsByUser(ctx context.Context, q Querier, userID string) (int64, error) {
	tag, err := q.Exec(ctx, "DELETE FROM turns WHERE user_id = $1", userID)
	if err != nil {
		return 0, fmt.Errorf("delete turns by user: %w", err)
	}
	return tag.RowsAffected(), nil
}
