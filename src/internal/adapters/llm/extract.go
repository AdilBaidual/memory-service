package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
)

// llmExtractionOutput is the JSON schema type for Structured Output (strict mode).
type llmExtractionOutput struct {
	Items         []llmExtractedItem `json:"items"`
	Relationships []llmRelationship  `json:"relationships"`
}

type llmExtractedItem struct {
	Type     string   `json:"type"`
	Key      string   `json:"key"`
	Value    string   `json:"value"`
	Evidence string   `json:"evidence"`
	Entities []string `json:"entities"`
}

type llmRelationship struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

// Extract calls gpt-4o-mini with Structured Output to extract candidate memories from a conversation turn.
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
		// Temperature field has omitempty — literal 0 would be dropped and the
		// API would use its default (~1.0). SmallestNonzeroFloat32 is non-zero
		// so it is sent, but is indistinguishable from 0 for the model.
		Temperature: math.SmallestNonzeroFloat32,
		MaxTokens:   1500,
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
	for _, rel := range output.Relationships {
		s := strings.TrimSpace(rel.Subject)
		p := strings.TrimSpace(rel.Predicate)
		o := strings.TrimSpace(rel.Object)
		if s == "" || p == "" || o == "" {
			continue
		}
		// Drop tautological triplets (X, P, X) — always a model error.
		if strings.EqualFold(s, o) {
			continue
		}
		result.Relationships = append(result.Relationships, Relationship{
			Subject:   s,
			Predicate: p,
			Object:    o,
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
			"relationships": {
				Type: jsonschema.Array,
				Items: &jsonschema.Definition{
					Type: jsonschema.Object,
					Properties: map[string]jsonschema.Definition{
						"subject":   strDef,
						"predicate": strDef,
						"object":    strDef,
					},
					Required:             []string{"subject", "predicate", "object"},
					AdditionalProperties: false,
				},
			},
		},
		Required:             []string{"items", "relationships"},
		AdditionalProperties: false,
	}
}
