// Package llm provides clients for LLM and reranking APIs.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const cohereRerankURL = "https://api.cohere.com/v2/rerank"

// CohereClient wraps the Cohere Rerank API.
// Nil when COHERE_API_KEY is not set — callers must handle nil.
type CohereClient struct {
	apiKey string
	model  string
	http   *http.Client
}

// NewCohereClient returns nil (not an error) when apiKey is empty.
// Callers treat nil as "reranker disabled" and skip gracefully.
func NewCohereClient(apiKey, model string) *CohereClient {
	if apiKey == "" {
		return nil
	}
	if model == "" {
		model = "rerank-english-v3.0"
	}
	return &CohereClient{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{},
	}
}

// RerankResult is a single reranked document with its new score.
type RerankResult struct {
	Index          int
	RelevanceScore float32
}

// Rerank calls the Cohere Rerank API and returns results sorted by
// relevance score descending. Returns nil (no error) if client is nil
// or documents is empty — callers fall back to original order.
func (c *CohereClient) Rerank(
	ctx context.Context,
	query string,
	documents []string,
	topN int,
) ([]RerankResult, error) {
	if c == nil {
		return nil, nil
	}
	if len(documents) == 0 {
		return nil, nil
	}
	if topN <= 0 || topN > len(documents) {
		topN = len(documents)
	}

	reqBody := map[string]any{
		"model":     c.model,
		"query":     query,
		"documents": documents,
		"top_n":     topN,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, cohereRerankURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cohere rerank request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cohere rerank status %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float32 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	out := make([]RerankResult, len(result.Results))
	for i, r := range result.Results {
		out[i] = RerankResult{
			Index:          r.Index,
			RelevanceScore: r.RelevanceScore,
		}
	}
	return out, nil
}
