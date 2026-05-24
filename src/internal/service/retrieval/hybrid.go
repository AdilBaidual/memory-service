// Package retrieval implements the hybrid retrieval pipeline.
package retrieval

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"golang.org/x/sync/errgroup"

	"memory-service/internal/adapters/llm"
	"memory-service/internal/adapters/store"
)

const (
	candidatesPerChannel = 30
	rerankCandidates     = 30

	// channelTimeout caps each retrieval channel individually so a slow graph
	// traversal doesn't hold up fusion — completed channels proceed regardless.
	channelTimeout = 30 * time.Second

	// temporalLambda controls decay rate: half-life ≈ 139 days.
	// Conservative — stable facts stay relevant even if not updated recently.
	temporalLambda = 0.005
	// temporalAlpha is the weight of the original RRF score.
	// Recency can add at most (1-alpha)=30% boost but cannot override semantic relevance.
	temporalAlpha = 0.7
)

// HybridRetriever runs semantic (cosine), keyword (FTS), and graph channels
// in parallel, fuses results with RRF, applies temporal boost, then optionally
// reranks with Cohere cross-encoder.
type HybridRetriever struct {
	pool   store.Querier
	client *llm.Client
	cohere *llm.CohereClient
}

func NewHybridRetriever(pool store.Querier, client *llm.Client, cohere *llm.CohereClient) *HybridRetriever {
	return &HybridRetriever{pool: pool, client: client, cohere: cohere}
}

// Retrieve runs semantic, keyword, and graph channels concurrently, fuses via
// RRF, applies temporal recency boost, optionally reranks with Cohere, and
// returns up to params.Limit memories.
func (r *HybridRetriever) Retrieve(
	ctx context.Context,
	params RetrieveParams,
) ([]RetrievedMemory, error) {
	if params.Limit <= 0 {
		params.Limit = 10
	}

	var (
		semanticResults []store.ScoredMemory
		keywordResults  []store.ScoredMemory
		graphResults    []store.ScoredMemory
	)

	// Each channel gets its own deadline derived from the request context.
	// The errgroup goroutine selects on chCtx.Done() so it returns immediately
	// on timeout — the inner worker goroutine cleans itself up via chCtx.
	// Buffered result channels (cap 1) prevent goroutine leaks.
	var g errgroup.Group

	type scoredResult struct {
		data []store.ScoredMemory
		err  error
	}

	if r.client != nil {
		g.Go(func() error {
			chCtx, cancel := context.WithTimeout(ctx, channelTimeout)
			defer cancel()
			done := make(chan scoredResult, 1)
			go func() {
				hydeQuery := r.hydeRewrite(chCtx, params.Query)
				emb, err := r.client.Embed(chCtx, hydeQuery)
				if err != nil {
					done <- scoredResult{err: err}
					return
				}
				data, err := store.GetTopKByCosine(
					chCtx, r.pool, params.Scope, emb, candidatesPerChannel)
				done <- scoredResult{data, err}
			}()
			select {
			case <-chCtx.Done():
				slog.Warn("semantic channel timed out", "err", chCtx.Err())
				return nil
			case res := <-done:
				if res.err != nil {
					slog.Warn("semantic channel failed", "err", res.err)
					return nil
				}
				semanticResults = res.data
				return nil
			}
		})
	}

	g.Go(func() error {
		chCtx, cancel := context.WithTimeout(ctx, channelTimeout)
		defer cancel()
		done := make(chan scoredResult, 1)
		go func() {
			data, err := keywordSearch(
				chCtx, r.pool, params.Scope, params.Query, candidatesPerChannel)
			done <- scoredResult{data, err}
		}()
		select {
		case <-chCtx.Done():
			slog.Warn("keyword channel timed out", "err", chCtx.Err())
			return nil
		case res := <-done:
			if res.err != nil {
				slog.Warn("keyword channel failed", "err", res.err)
				return nil
			}
			keywordResults = res.data
			return nil
		}
	})

	g.Go(func() error {
		chCtx, cancel := context.WithTimeout(ctx, channelTimeout)
		defer cancel()
		done := make(chan scoredResult, 1)
		go func() {
			data, err := graphSearch(
				chCtx, r.pool, params.Scope, params.Query, candidatesPerChannel)
			done <- scoredResult{data, err}
		}()
		select {
		case <-chCtx.Done():
			slog.Warn("graph channel timed out", "err", chCtx.Err())
			return nil
		case res := <-done:
			if res.err != nil {
				slog.Warn("graph channel failed", "err", res.err)
				return nil
			}
			graphResults = res.data
			return nil
		}
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	var inputs []RRFInput
	if len(semanticResults) > 0 {
		inputs = append(inputs, RRFInput{Memories: semanticResults})
	}
	if len(keywordResults) > 0 {
		inputs = append(inputs, RRFInput{Memories: keywordResults})
	}
	if len(graphResults) > 0 {
		inputs = append(inputs, RRFInput{Memories: graphResults})
	}

	if len(inputs) == 0 {
		return nil, nil
	}

	// Pass enough candidates for reranker; final slice is trimmed to Limit below.
	fuseCandidates := rerankCandidates
	if params.Limit > fuseCandidates {
		fuseCandidates = params.Limit
	}
	fused := FuseRRF(inputs, fuseCandidates)

	boosted := applyTemporalBoost(fused, time.Now())
	reranked := r.applyReranker(ctx, params.Query, boosted, params.Limit)

	if len(reranked) > params.Limit {
		reranked = reranked[:params.Limit]
	}

	result := make([]RetrievedMemory, len(reranked))
	for i, m := range reranked {
		result[i] = RetrievedMemory{ScoredMemory: m}
	}
	return result, nil
}

// applyTemporalBoost adjusts RRF scores based on memory recency.
// final = rrf_score * (alpha + (1-alpha) * exp(-lambda * days_since_update))
func applyTemporalBoost(memories []store.ScoredMemory, now time.Time) []store.ScoredMemory {
	out := make([]store.ScoredMemory, len(memories))
	for i, m := range memories {
		days := now.Sub(m.UpdatedAt).Hours() / 24
		recency := math.Exp(-temporalLambda * days)
		m.Score = m.Score * float32(temporalAlpha+(1-temporalAlpha)*recency)
		out[i] = m
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	return out
}

// applyReranker reranks fused results using Cohere cross-encoder.
// Returns original order if cohere client is nil or on error.
func (r *HybridRetriever) applyReranker(
	ctx context.Context,
	query string,
	memories []store.ScoredMemory,
	finalLimit int,
) []store.ScoredMemory {
	if r.cohere == nil || len(memories) == 0 {
		return memories
	}

	docs := make([]string, len(memories))
	for i, m := range memories {
		docs[i] = m.Value
	}

	results, err := r.cohere.Rerank(ctx, query, docs, finalLimit)
	if err != nil {
		slog.Warn("cohere rerank failed, using RRF order", "err", err)
		return memories
	}
	if len(results) == 0 {
		return memories
	}

	slog.Info("cohere rerank applied", "candidates", len(memories), "returned", len(results))

	reranked := make([]store.ScoredMemory, len(results))
	for i, res := range results {
		m := memories[res.Index]
		m.Score = res.RelevanceScore
		reranked[i] = m
	}
	return reranked
}

const hydeSystemPrompt = `You are helping a memory retrieval system.
Given a question about a person, write a single sentence that represents
a plausible factual answer about that person. Write as if you know the
answer. Be specific and concrete.

Examples:
Q: "What does the user do for work?"
A: "The user works as a software engineer at a technology company."

Q: "Where does the user live?"
A: "The user lives in a city in Europe."

Q: "What is the user's opinion on TypeScript?"
A: "The user believes TypeScript is useful for large projects but adds complexity for small teams."

Return only the hypothetical answer sentence, nothing else.`

// hydeRewrite generates a hypothetical answer to embed instead of the raw query.
// Applied only to the semantic channel; FTS and graph use the original query.
// Returns original query on any error (graceful degradation).
func (r *HybridRetriever) hydeRewrite(ctx context.Context, query string) string {
	if r.client == nil {
		return query
	}
	resp, err := r.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: hydeSystemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: query},
		},
		MaxTokens:   80,
		Temperature: 0,
	})
	if err != nil || len(resp.Choices) == 0 {
		slog.Warn("hyde rewrite failed, using original query", "err", err)
		return query
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content)
}
