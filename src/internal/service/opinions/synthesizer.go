// Package opinions implements opinion_view synthesis — aggregating a user's
// raw opinion statements into a single synthesized paragraph per topic.
package opinions

import (
	"context"
	"fmt"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"memory-service/internal/adapters/llm"
)

// SynthesisRequest carries all raw opinion statements for a single user+topic.
type SynthesisRequest struct {
	UserID   string
	Key      string   // e.g. "typescript_view"
	Opinions []string // raw opinion values in chronological order
}

// SynthesisResult holds the single synthesized paragraph.
type SynthesisResult struct {
	Value string
}

// Synthesizer generates opinion_view text from raw opinions.
type Synthesizer interface {
	Synthesize(ctx context.Context, req SynthesisRequest) (*SynthesisResult, error)
}

// LLMSynthesizer implements Synthesizer via gpt-4o-mini.
type LLMSynthesizer struct {
	client *llm.Client
}

// NewLLMSynthesizer creates a new LLMSynthesizer. client may be nil (no-op).
func NewLLMSynthesizer(client *llm.Client) *LLMSynthesizer {
	return &LLMSynthesizer{client: client}
}

const synthesisSystemPrompt = `You are summarizing how a person's view on a topic has evolved over
time based on their statements in chronological order.

Write a single paragraph (2-4 sentences) that:
- Captures the current settled position (most weight on recent statements)
- Notes how the view evolved if it changed significantly
- Uses third person ("The user...")
- Is factual and neutral in tone

Do not editorialize. Do not add opinions not present in the statements.
Return only the paragraph, no preamble.`

// Synthesize calls the LLM to produce a synthesized opinion_view paragraph.
func (s *LLMSynthesizer) Synthesize(ctx context.Context, req SynthesisRequest) (*SynthesisResult, error) {
	if s.client == nil {
		return nil, fmt.Errorf("llm client not configured")
	}

	var sb strings.Builder
	sb.WriteString("Topic: ")
	sb.WriteString(req.Key)
	sb.WriteString("\n\nStatements in chronological order:\n")
	for i, op := range req.Opinions {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, op)
	}
	sb.WriteString("\nWrite a synthesis paragraph.")

	resp, err := s.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: synthesisSystemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: sb.String()},
		},
		MaxTokens:   200,
		Temperature: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("synthesis llm call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty synthesis response from llm")
	}
	return &SynthesisResult{Value: strings.TrimSpace(resp.Choices[0].Message.Content)}, nil
}
