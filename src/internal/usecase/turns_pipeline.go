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
	"memory-service/internal/service/opinions"
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

		// Insert entity_mentions for each entity so the graph channel can find
		// this memory directly, independent of relationship anchor resolution.
		if memID != uuid.Nil {
			for _, ent := range c.Entities {
				lower := strings.ToLower(strings.TrimSpace(ent))
				if lower == "" || lower == "user" {
					continue
				}
				if err := store.UpsertEntity(ctx, tx2, lower, *in.UserID, ""); err != nil {
					slog.Warn("upsert entity for mention failed",
						"entity", lower, "err", err, "user_id", *in.UserID)
					continue
				}
				if err := store.InsertEntityMention(ctx, tx2, memID, lower, *in.UserID, "entity"); err != nil {
					slog.Warn("insert entity mention failed",
						"entity", lower, "memory_id", memID, "err", err, "user_id", *in.UserID)
				}
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
		return
	}
	slog.Info("memories inserted",
		"count", inserted, "turn_id", turnID.String(), "user_id", *in.UserID)

	// Opinion synthesis: runs after commit so raw opinions are visible.
	// Non-fatal — synthesis failures never prevent turn ingestion from succeeding.
	if uc.opinSynth != nil {
		uc.synthesizeOpinionViews(ctx, candidates, *in.UserID, in.SessionID)
	}
}

// synthesizeOpinionViews checks all opinion keys added this turn and generates
// or updates a synthesized opinion_view when 2+ raw opinions exist.
func (uc *IngestTurnUsecase) synthesizeOpinionViews(
	ctx context.Context,
	candidates []extraction.Candidate,
	userID, sessionID string,
) {
	keys := collectOpinionKeys(candidates)
	for _, key := range keys {
		rawOps, err := store.GetRawOpinionsByKey(ctx, uc.pool, userID, key)
		if err != nil {
			slog.Warn("get raw opinions failed", "key", key, "err", err, "user_id", userID)
			continue
		}
		if len(rawOps) < 2 {
			continue
		}

		values := make([]string, len(rawOps))
		for i, op := range rawOps {
			values[i] = op.Value
		}

		result, err := uc.opinSynth.Synthesize(ctx, opinions.SynthesisRequest{
			UserID:   userID,
			Key:      key,
			Opinions: values,
		})
		if err != nil {
			slog.Warn("opinion synthesis failed", "key", key, "err", err, "user_id", userID)
			continue
		}

		// Embed the synthesized value so recall can filter by relevance.
		var embedding []float32
		if uc.ext != nil {
			embCtx, embCancel := context.WithTimeout(ctx, 10*time.Second)
			embedding, _ = uc.ext.Embed(embCtx, result.Value)
			embCancel()
		}

		tx3, err := uc.pool.Begin(ctx)
		if err != nil {
			slog.Warn("begin opinion_view tx failed", "key", key, "err", err)
			continue
		}
		if err := store.UpsertOpinionView(ctx, tx3, userID, key, result.Value, &sessionID, embedding); err != nil {
			tx3.Rollback(ctx) //nolint:errcheck
			slog.Warn("upsert opinion_view failed", "key", key, "err", err, "user_id", userID)
			continue
		}
		if err := tx3.Commit(ctx); err != nil {
			slog.Warn("commit opinion_view tx failed", "key", key, "err", err)
		} else {
			slog.Info("opinion_view upserted", "key", key, "user_id", userID)
		}
	}
}

// collectOpinionKeys returns deduplicated keys from opinion candidates.
func collectOpinionKeys(candidates []extraction.Candidate) []string {
	seen := make(map[string]bool)
	var keys []string
	for _, c := range candidates {
		if c.Type == "opinion" && c.Key != nil && *c.Key != "" {
			k := *c.Key
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	return keys
}
