package llm

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

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
