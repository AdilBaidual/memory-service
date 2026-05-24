package handlers

import (
	"encoding/json"
	"time"
)

type Message struct {
	Role    string  `json:"role"`
	Content string  `json:"content"`
	Name    *string `json:"name,omitempty"`
}

type Citation struct {
	TurnID  string  `json:"turn_id"`
	Score   float32 `json:"score"`
	Snippet string  `json:"snippet"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type TurnRequest struct {
	SessionID string          `json:"session_id"`
	UserID    *string         `json:"user_id"`
	Messages  []Message       `json:"messages"`
	Timestamp time.Time       `json:"timestamp"`
	Metadata  json.RawMessage `json:"metadata"`
}

type TurnResponse struct {
	ID string `json:"id"`
}

type RecallRequest struct {
	Query     string  `json:"query"`
	SessionID string  `json:"session_id"`
	UserID    *string `json:"user_id"`
	MaxTokens int     `json:"max_tokens"`
}

type RecallResponse struct {
	Context   string     `json:"context"`
	Citations []Citation `json:"citations"`
}

type SearchRequest struct {
	Query     string  `json:"query"`
	SessionID *string `json:"session_id"`
	UserID    *string `json:"user_id"`
	Limit     int     `json:"limit"`
}

type SearchResult struct {
	Content   string          `json:"content"`
	Score     float32         `json:"score"`
	SessionID string          `json:"session_id"`
	Timestamp time.Time       `json:"timestamp"`
	Metadata  json.RawMessage `json:"metadata"`
}

type SearchResponse struct {
	Results []SearchResult `json:"results"`
}

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

type MemoriesListResponse struct {
	Memories []MemoryView `json:"memories"`
	Total    int          `json:"total"`
	Limit    int          `json:"limit"`
	Offset   int          `json:"offset"`
}
