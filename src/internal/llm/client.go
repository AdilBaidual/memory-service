// Package llm provides an OpenAI client wrapper for extraction, embeddings,
// and representation updates.
package llm

import (
	openai "github.com/sashabaranov/go-openai"

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
