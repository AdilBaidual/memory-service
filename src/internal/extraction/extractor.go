// Package extraction handles LLM-based extraction of structured memories
// from raw conversation turns.
package extraction

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"memory-service/internal/llm"
	"memory-service/internal/storage"
)

// Extractor orchestrates LLM-based memory extraction.
type Extractor struct {
	client *llm.Client
}

// New creates an Extractor backed by the given LLM client.
func New(client *llm.Client) *Extractor {
	return &Extractor{client: client}
}

// Extract runs LLM extraction on a turn and returns memory candidates
// and relationship triplets.
// Returns nil, nil, nil when the LLM client is not configured — callers should
// treat this as graceful degradation (turn saved, no memories extracted).
func (e *Extractor) Extract(
	ctx context.Context,
	pool storage.Querier,
	userID string,
	messages []storage.TurnMessage,
) ([]Candidate, []llm.Relationship, error) {
	if e.client == nil {
		return nil, nil, nil
	}

	existingKeys, err := storage.GetCanonicalKeys(ctx, pool, userID)
	if err != nil {
		slog.Warn("get canonical keys failed, proceeding without hints", "error", err, "user_id", userID)
		existingKeys = nil
	}

	existingTopics, err := storage.GetOpinionTopics(ctx, pool, userID)
	if err != nil {
		slog.Warn("get opinion topics failed, proceeding without hints", "error", err, "user_id", userID)
		existingTopics = nil
	}

	req := llm.ExtractionRequest{
		Conversation:          formatConversation(messages),
		ExistingKeys:          existingKeys,
		ExistingOpinionTopics: existingTopics,
	}

	result, err := e.client.Extract(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("llm extract: %w", err)
	}

	var candidates []Candidate
	for _, item := range result.Items {
		if strings.TrimSpace(item.Value) == "" {
			continue
		}
		memType := normalizeType(item.Type)
		if memType != item.Type {
			slog.Warn("llm returned unexpected type, normalized",
				"raw", item.Type, "normalized", memType, "user_id", userID)
		}
		evidence := normalizeEvidence(item.Evidence)
		if evidence != item.Evidence {
			slog.Warn("llm returned unexpected evidence, normalized",
				"raw", item.Evidence, "normalized", evidence, "user_id", userID)
		}
		key := strings.TrimSpace(item.Key)
		candidates = append(candidates, Candidate{
			Type:       memType,
			Key:        nullableKey(key),
			Value:      item.Value,
			Evidence:   evidence,
			Entities:   item.Entities,
			Confidence: computeConfidence(evidence),
		})
	}
	return candidates, result.Relationships, nil
}

// Embed embeds a single text using the underlying LLM client.
// Returns nil, nil when the client is not configured.
func (e *Extractor) Embed(ctx context.Context, text string) ([]float32, error) {
	if e.client == nil {
		return nil, nil
	}
	return e.client.Embed(ctx, text)
}

// formatConversation serializes messages as "role: content" lines.
// Tool messages are prefixed with [tool] to signal context-only status.
func formatConversation(messages []storage.TurnMessage) string {
	var sb strings.Builder
	for _, m := range messages {
		switch m.Role {
		case "tool":
			sb.WriteString("[tool]: ")
		default:
			sb.WriteString(m.Role)
			sb.WriteString(": ")
		}
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// computeConfidence returns a confidence score based on the evidence type.
// Confidence is computed by the system; it is never requested from the LLM.
// Expects a normalised value ("explicit" or "implicit") — call normalizeEvidence first.
func computeConfidence(evidence string) float32 {
	switch evidence {
	case "explicit":
		return 0.95
	case "implicit":
		return 0.70
	default:
		return 0.80
	}
}

// normalizeType coerces the LLM-returned type to one of the valid enum values.
// Falls back to "fact" on anything unexpected.
func normalizeType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "fact", "preference", "opinion", "event":
		return strings.ToLower(strings.TrimSpace(s))
	}
	return "fact"
}

// normalizeEvidence coerces the LLM-returned evidence to "explicit" or "implicit".
// If the raw value contains either keyword it is recovered; otherwise defaults to "implicit".
func normalizeEvidence(s string) string {
	lower := strings.ToLower(strings.TrimSpace(s))
	if lower == "explicit" || lower == "implicit" {
		return lower
	}
	if strings.Contains(lower, "explicit") {
		return "explicit"
	}
	return "implicit"
}

func nullableKey(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
