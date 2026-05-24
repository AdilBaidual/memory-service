// Package llm provides an OpenAI client wrapper for extraction, embeddings,
// and representation updates.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"

	"memory-service/internal/config"
)

// Client wraps the OpenAI SDK with our configuration.
// Returns nil if no API key is configured (degraded mode).
// Callers must check for nil before using.
type Client struct {
	openai *openai.Client
	cfg    *config.Config
}

// NewClient creates a Client from config.
// Returns nil when OPENAI_API_KEY is not set.
func NewClient(cfg *config.Config) *Client {
	if cfg.OpenAIAPIKey == "" {
		return nil
	}
	return &Client{
		openai: openai.NewClient(cfg.OpenAIAPIKey),
		cfg:    cfg,
	}
}

// Embed returns a 1536-dimensional vector for the given text using
// text-embedding-3-small. Returns an error if the client is nil.
func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	if c == nil {
		return nil, fmt.Errorf("llm client not configured: OPENAI_API_KEY is not set")
	}

	resp, err := c.openai.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: openai.EmbeddingModel(c.cfg.OpenAIEmbeddingModel),
	})
	if err != nil {
		return nil, fmt.Errorf("create embedding: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return resp.Data[0].Embedding, nil
}

// llmExtractionOutput is the JSON schema type for Structured Output (strict mode).
type llmExtractionOutput struct {
	Items []llmExtractedItem `json:"items"`
}

type llmExtractedItem struct {
	Type     string   `json:"type"`
	Key      string   `json:"key"`
	Value    string   `json:"value"`
	Evidence string   `json:"evidence"`
	Entities []string `json:"entities"`
}

// Extract calls gpt-4o-mini with Structured Output to extract candidate
// memories from a conversation turn. Returns an error if the client is nil.
func (c *Client) Extract(ctx context.Context, req ExtractionRequest) (*ExtractionResult, error) {
	if c == nil {
		return nil, fmt.Errorf("llm client not configured: OPENAI_API_KEY is not set")
	}

	var result *ExtractionResult
	err := withRetry(ctx, 4, func() error {
		r, err := c.doExtract(ctx, req)
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	return result, err
}

func (c *Client) doExtract(ctx context.Context, req ExtractionRequest) (*ExtractionResult, error) {
	schema := extractionSchema()
	prompt := buildExtractionPrompt(req)

	resp, err := c.openai.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.cfg.OpenAIExtractionModel,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: extractionSystemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   "extraction_result",
				Schema: schema,
				Strict: true,
			},
		},
		MaxTokens: 1500,
	})
	if err != nil {
		return nil, fmt.Errorf("openai extraction: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices from openai")
	}

	var output llmExtractionOutput
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &output); err != nil {
		return nil, fmt.Errorf("parse extraction output: %w", err)
	}

	result := &ExtractionResult{}
	for _, item := range output.Items {
		result.Items = append(result.Items, ExtractedItem{
			Type:     item.Type,
			Key:      item.Key,
			Value:    item.Value,
			Evidence: item.Evidence,
			Entities: item.Entities,
		})
	}
	return result, nil
}

// extractionSchema returns a hand-written JSON schema for llmExtractionOutput.
// Replaces GenerateSchemaForType to add enum constraints on type and evidence —
// without them the model occasionally returns free-text in those fields.
func extractionSchema() *jsonschema.Definition {
	strDef := jsonschema.Definition{Type: jsonschema.String}
	return &jsonschema.Definition{
		Type: jsonschema.Object,
		Properties: map[string]jsonschema.Definition{
			"items": {
				Type: jsonschema.Array,
				Items: &jsonschema.Definition{
					Type: jsonschema.Object,
					Properties: map[string]jsonschema.Definition{
						"type": {
							Type: jsonschema.String,
							Enum: []string{"fact", "preference", "opinion", "event"},
						},
						"key":   strDef,
						"value": strDef,
						"evidence": {
							Type: jsonschema.String,
							Enum: []string{"explicit", "implicit"},
						},
						"entities": {
							Type:  jsonschema.Array,
							Items: &strDef,
						},
					},
					Required:             []string{"type", "key", "value", "evidence", "entities"},
					AdditionalProperties: false,
				},
			},
		},
		Required:             []string{"items"},
		AdditionalProperties: false,
	}
}

// withRetry retries fn up to maxAttempts times on retryable errors.
// Backoff between attempts: 1s, 2s, 4s. Respects ctx cancellation during sleep.
func withRetry(ctx context.Context, maxAttempts int, fn func() error) error {
	backoff := time.Second
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isRetryable(err) {
			return err
		}
		if i < maxAttempts-1 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
			backoff *= 2
		}
	}
	return lastErr
}

// isRetryable returns true for transient errors worth retrying.
// Returns false for 400 (bad request) and 401 (auth) errors.
func isRetryable(err error) bool {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case 429, 500, 502, 503, 504:
			return true
		default:
			return false
		}
	}
	// Non-API errors (network errors) are retryable.
	return true
}
