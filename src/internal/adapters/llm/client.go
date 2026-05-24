// Package llm provides an OpenAI client wrapper for extraction, embeddings,
// and representation updates.
package llm

import (
	"context"
	"errors"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"memory-service/internal/config"
)

// Client wraps the OpenAI SDK with our configuration.
// Returns nil if no API key is configured (degraded mode).
// All methods on *Client must guard against c == nil at the top of the function.
type Client struct {
	openai *openai.Client
	cfg    *config.Config
}

// NewClient returns nil when OPENAI_API_KEY is not set.
func NewClient(cfg *config.Config) *Client {
	if cfg.OpenAIAPIKey == "" {
		return nil
	}
	return &Client{
		openai: openai.NewClient(cfg.OpenAIAPIKey),
		cfg:    cfg,
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
	return true
}
