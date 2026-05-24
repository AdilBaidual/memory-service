package api

import (
	"encoding/json"
	"time"
)

// Message is a single turn message from the conversation log.
type Message struct {
	Role    string  `json:"role"`
	Content string  `json:"content"`
	Name    *string `json:"name,omitempty"`
}

// Citation references a source turn that contributed to a recall response.
type Citation struct {
	TurnID  string  `json:"turn_id"`
	Score   float32 `json:"score"`
	Snippet string  `json:"snippet"`
}

// ErrorResponse is the JSON body for 4xx/5xx responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// TurnRequest is the body for POST /turns.
type TurnRequest struct {
	SessionID string          `json:"session_id"`
	UserID    *string         `json:"user_id"`
	Messages  []Message       `json:"messages"`
	Timestamp time.Time       `json:"timestamp"`
	Metadata  json.RawMessage `json:"metadata"`
}

// TurnResponse is the body for a successful POST /turns.
type TurnResponse struct {
	ID string `json:"id"`
}

// RecallRequest is the body for POST /recall.
type RecallRequest struct {
	Query     string  `json:"query"`
	SessionID string  `json:"session_id"`
	UserID    *string `json:"user_id"`
	MaxTokens int     `json:"max_tokens"`
}

// RecallResponse is the body for POST /recall.
type RecallResponse struct {
	Context   string     `json:"context"`
	Citations []Citation `json:"citations"`
}

// SearchRequest is the body for POST /search.
type SearchRequest struct {
	Query     string  `json:"query"`
	SessionID *string `json:"session_id"`
	UserID    *string `json:"user_id"`
	Limit     int     `json:"limit"`
}

// SearchResult is one item in a search response.
type SearchResult struct {
	Content   string          `json:"content"`
	Score     float32         `json:"score"`
	SessionID string          `json:"session_id"`
	Timestamp time.Time       `json:"timestamp"`
	Metadata  json.RawMessage `json:"metadata"`
}

// SearchResponse is the body for POST /search.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

// MemoryView is the read-only representation of a memory returned by
// GET /users/{user_id}/memories.
type MemoryView struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Key           *string         `json:"key"`
	Value         string          `json:"value"`
	Confidence    float32         `json:"confidence"`
	Evidence      string          `json:"evidence"`
	Entities      json.RawMessage `json:"entities"`
	SourceSession *string         `json:"source_session"`
	SourceTurn    *string         `json:"source_turn"`
	ValidFrom     *time.Time      `json:"valid_from"`
	ValidTo       *time.Time      `json:"valid_to"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Supersedes    *string         `json:"supersedes"`
	Active        bool            `json:"active"`
	Metadata      json.RawMessage `json:"metadata"`
}

// MemoriesListResponse is the body for GET /users/{user_id}/memories.
type MemoriesListResponse struct {
	Memories []MemoryView `json:"memories"`
}
