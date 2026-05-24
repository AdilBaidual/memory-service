// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
	"memory-service/internal/service/consolidation"
	"memory-service/internal/service/extraction"
	"memory-service/internal/service/opinions"
)

// TxPool abstracts the DB pool for transaction-managed persistence.
// *pgxpool.Pool satisfies this interface automatically.
type TxPool interface {
	store.Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

// ExtractionService extracts structured memories from conversation text.
type ExtractionService interface {
	Extract(ctx context.Context, input extraction.ExtractionInput) ([]extraction.Candidate, []llm.Relationship, error)
	Embed(ctx context.Context, text string) ([]float32, error)
}

// ConsolidationService applies ADD/NOOP/UPDATE logic for a single fact candidate.
type ConsolidationService interface {
	ConsolidateFact(
		ctx context.Context,
		q store.Querier,
		userID, memType string,
		key *string,
		value, evidence string,
		confidence float32,
		entities []string,
		embedding []float32,
		sourceSession *string,
		sourceTurn *uuid.UUID,
	) (uuid.UUID, consolidation.Result, error)
}

type RelationshipsService interface {
	ProcessRelationships(
		ctx context.Context,
		q store.Querier,
		userID string,
		rels []llm.Relationship,
		entityMemoryMap map[string]uuid.UUID,
		fallbackMemoryID uuid.UUID,
	) error
}

// OpinionSynthesizer generates synthesized opinion_view from raw opinions.
type OpinionSynthesizer interface {
	Synthesize(ctx context.Context, req opinions.SynthesisRequest) (*opinions.SynthesisResult, error)
}

type TurnMessage struct {
	Role    string  `json:"role"`
	Content string  `json:"content"`
	Name    *string `json:"name,omitempty"`
}

type TurnInput struct {
	SessionID string
	UserID    *string
	Messages  []TurnMessage
	Timestamp time.Time
	Metadata  json.RawMessage
}

type TurnOutput struct {
	ID string
}

// IngestTurnUsecase persists a conversation turn and drives synchronous extraction.
// It uses TxPool to begin and commit transactions across multi-step persistence.
// All service calls accept store.Querier, which pgx.Tx satisfies.
type IngestTurnUsecase struct {
	pool      TxPool
	ext       ExtractionService
	cons      ConsolidationService
	relProc   RelationshipsService
	opinSynth OpinionSynthesizer // may be nil
}

// NewIngestTurnUsecase creates an IngestTurnUsecase.
// Pass nil for ext/opinSynth to skip those features when no LLM key is available.
func NewIngestTurnUsecase(
	pool TxPool,
	ext ExtractionService,
	cons ConsolidationService,
	relProc RelationshipsService,
	opinSynth OpinionSynthesizer,
) *IngestTurnUsecase {
	return &IngestTurnUsecase{pool: pool, ext: ext, cons: cons, relProc: relProc, opinSynth: opinSynth}
}

// Ingest saves the turn synchronously within the same request.
// Extraction errors are non-fatal: the turn ID is returned regardless.
func (uc *IngestTurnUsecase) Ingest(ctx context.Context, in TurnInput) (TurnOutput, error) {
	messagesJSON, err := json.Marshal(in.Messages)
	if err != nil {
		return TurnOutput{}, fmt.Errorf("marshal messages: %w", err)
	}

	metadata := in.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}

	tx, err := uc.pool.Begin(ctx)
	if err != nil {
		return TurnOutput{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	turnID, err := store.InsertTurn(ctx, tx, store.InsertTurnParams{
		SessionID: in.SessionID,
		UserID:    in.UserID,
		Messages:  messagesJSON,
		Timestamp: in.Timestamp,
		Metadata:  metadata,
	})
	if err != nil {
		return TurnOutput{}, fmt.Errorf("insert turn: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return TurnOutput{}, fmt.Errorf("commit transaction: %w", err)
	}

	userID := ""
	if in.UserID != nil {
		userID = *in.UserID
	}
	slog.Info("turn inserted",
		"turn_id", turnID.String(),
		"session_id", in.SessionID,
		"user_id", userID,
		"message_count", len(in.Messages),
	)

	if in.UserID == nil {
		return TurnOutput{ID: turnID.String()}, nil
	}

	uc.extractAndPersist(ctx, in, turnID)
	return TurnOutput{ID: turnID.String()}, nil
}
