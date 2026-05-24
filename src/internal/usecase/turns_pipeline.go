package usecase

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
	"memory-service/internal/service/consolidation"
	"memory-service/internal/service/extraction"
)

// Non-fatal: all errors are logged, never returned.
func (uc *IngestTurnUsecase) extractAndPersist(ctx context.Context, in TurnInput, turnID uuid.UUID) {
	if uc.ext == nil {
		return
	}
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
