// Package usecase implements the business-logic layer for the memory service.
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
	"memory-service/internal/service/consolidation"
	"memory-service/internal/service/extraction"
)

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

// RelationshipsService persists entity triplets from a turn.
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

// TurnMessage is a single message in a conversation turn.
type TurnMessage struct {
	Role    string  `json:"role"`
	Content string  `json:"content"`
	Name    *string `json:"name,omitempty"`
}

// TurnInput carries the parsed turn data for ingestion.
type TurnInput struct {
	SessionID string
	UserID    *string
	Messages  []TurnMessage
	Timestamp time.Time
	Metadata  json.RawMessage
}

// TurnOutput carries the result of turn ingestion.
type TurnOutput struct {
	ID string
}

// IngestTurnUsecase handles the full turn ingestion pipeline.
// It holds *pgxpool.Pool directly because it must Begin/Commit/Rollback
// across multi-step persistence. All service calls accept store.Querier,
// which pgx.Tx satisfies.
type IngestTurnUsecase struct {
	pool    *pgxpool.Pool
	ext     ExtractionService
	cons    ConsolidationService
	relProc RelationshipsService
}

// NewIngestTurnUsecase creates an IngestTurnUsecase.
// Pass nil for ext to skip extraction (e.g. when no LLM key is available).
func NewIngestTurnUsecase(
	pool *pgxpool.Pool,
	ext ExtractionService,
	cons ConsolidationService,
	relProc RelationshipsService,
) *IngestTurnUsecase {
	return &IngestTurnUsecase{pool: pool, ext: ext, cons: cons, relProc: relProc}
}

// Ingest saves the turn and, when a user and extractor are present,
// extracts and consolidates memories synchronously within the same request.
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

// extractAndPersist runs LLM extraction and persists the results.
// Non-fatal: all errors are logged, never returned.
func (uc *IngestTurnUsecase) extractAndPersist(ctx context.Context, in TurnInput, turnID uuid.UUID) {
	extMsgs := make([]extraction.Message, len(in.Messages))
	for i, m := range in.Messages {
		extMsgs[i] = extraction.Message{Role: m.Role, Content: m.Content}
	}

	existingKVs, kvErr := store.GetCanonicalKeyValues(ctx, uc.pool, *in.UserID)
	if kvErr != nil {
		slog.Warn("get canonical key values failed, proceeding without hints",
			"error", kvErr, "user_id", *in.UserID)
	}
	existingTopics, topicsErr := store.GetOpinionTopics(ctx, uc.pool, *in.UserID)
	if topicsErr != nil {
		slog.Warn("get opinion topics failed, proceeding without hints",
			"error", topicsErr, "user_id", *in.UserID)
	}

	kvHints := make([]extraction.KeyValue, len(existingKVs))
	for i, kv := range existingKVs {
		kvHints[i] = extraction.KeyValue{Key: kv.Key, Value: kv.Value}
	}

	llmCtx, llmCancel := context.WithTimeout(ctx, 30*time.Second)
	defer llmCancel()
	candidates, rels, err := uc.ext.Extract(llmCtx, extraction.ExtractionInput{
		Conversation:          extraction.FormatConversation(extMsgs),
		ExistingKeyValues:     kvHints,
		ExistingOpinionTopics: existingTopics,
	})
	if err != nil {
		slog.Warn("extraction failed", "error", err, "user_id", *in.UserID)
		return
	}
	if len(candidates) == 0 && len(rels) == 0 {
		slog.Info("no candidates extracted", "user_id", *in.UserID)
		return
	}

	uc.persistMemories(ctx, in, turnID, candidates, rels)
}

// persistMemories embeds each candidate and saves memories in a second transaction.
// Non-fatal: all errors are logged, never returned.
func (uc *IngestTurnUsecase) persistMemories(
	ctx context.Context,
	in TurnInput,
	turnID uuid.UUID,
	candidates []extraction.Candidate,
	rels []llm.Relationship,
) {
	tx2, err := uc.pool.Begin(ctx)
	if err != nil {
		slog.Error("begin memory transaction", "error", err, "user_id", *in.UserID)
		return
	}
	defer tx2.Rollback(ctx) //nolint:errcheck

	// entityAnchors tracks the best source memory for each entity name.
	// Facts and preferences take priority over events and opinions —
	// LLM output order must not determine which memory anchors a relationship.
	type entityAnchor struct {
		memID    uuid.UUID
		fromFact bool
	}
	entityAnchors := make(map[string]entityAnchor)

	inserted := 0
	var lastMemoryID uuid.UUID

	for _, c := range candidates {
		if ctx.Err() != nil {
			break
		}
		embCtx, embCancel := context.WithTimeout(ctx, 10*time.Second)
		embedding, embErr := uc.ext.Embed(embCtx, c.Value)
		embCancel()
		if embErr != nil {
			slog.Warn("embed candidate failed, storing without vector",
				"error", embErr, "user_id", *in.UserID)
			embedding = nil
		}

		var memID uuid.UUID
		switch c.Type {
		case "fact", "preference":
			id, result, consErr := uc.cons.ConsolidateFact(ctx, tx2,
				*in.UserID, c.Type, c.Key, c.Value, c.Evidence, c.Confidence,
				c.Entities, embedding, &in.SessionID, &turnID,
			)
			if consErr != nil {
				slog.Warn("consolidation failed",
					"type", c.Type, "key", c.Key, "err", consErr, "user_id", *in.UserID)
				continue
			}
			memID = id
			if result != consolidation.ResultNOOP {
				lastMemoryID = id
			}
			inserted++
			slog.Debug("memory consolidated",
				"result", result.String(), "type", c.Type, "key", c.Key, "user_id", *in.UserID)

		case "opinion", "event":
			entJSON, _ := json.Marshal(c.Entities)
			id, insErr := store.InsertMemory(ctx, tx2, store.InsertMemoryParams{
				UserID:        *in.UserID,
				Type:          c.Type,
				Key:           c.Key,
				Value:         c.Value,
				Evidence:      c.Evidence,
				Confidence:    c.Confidence,
				Entities:      json.RawMessage(entJSON),
				Embedding:     embedding,
				SourceSession: &in.SessionID,
				SourceTurn:    &turnID,
				Supersedes:    nil,
			})
			if insErr != nil {
				slog.Warn("insert failed",
					"type", c.Type, "key", c.Key, "err", insErr, "user_id", *in.UserID)
				continue
			}
			memID = id
			lastMemoryID = id
			inserted++
			slog.Debug("memory inserted", "type", c.Type, "key", c.Key, "user_id", *in.UserID)
		}

		fromFact := c.Type == "fact" || c.Type == "preference"
		for _, ent := range c.Entities {
			lower := strings.ToLower(strings.TrimSpace(ent))
			if lower == "" || lower == "user" {
				continue
			}
			existing, exists := entityAnchors[lower]
			if !exists || (!existing.fromFact && fromFact) {
				entityAnchors[lower] = entityAnchor{memID: memID, fromFact: fromFact}
			}
		}
	}

	entityMemoryMap := make(map[string]uuid.UUID, len(entityAnchors))
	for k, v := range entityAnchors {
		entityMemoryMap[k] = v.memID
	}

	if len(rels) > 0 {
		if err := uc.relProc.ProcessRelationships(
			ctx, tx2, *in.UserID, rels, entityMemoryMap, lastMemoryID,
		); err != nil {
			slog.Warn("relationship processing failed",
				"err", err, "user_id", *in.UserID, "count", len(rels))
		} else {
			slog.Debug("relationships processed",
				"count", len(rels), "user_id", *in.UserID)
		}
	}

	if err := tx2.Commit(ctx); err != nil {
		slog.Error("commit memory transaction", "error", err, "user_id", *in.UserID)
	} else {
		slog.Info("memories inserted",
			"count", inserted, "turn_id", turnID.String(), "user_id", *in.UserID)
	}
}
