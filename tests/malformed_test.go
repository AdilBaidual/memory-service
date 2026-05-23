// Package tests contains component tests for the memory service HTTP API.
package tests

import (
	"fmt"
	"strings"
	"testing"
)

func TestMalformed_BrokenJSON(t *testing.T) {
	resp := postRaw(t, "/turns", "application/json", []byte(`{"foo":`))
	mustStatus(t, resp, 400)
}

func TestMalformed_MissingSessionID(t *testing.T) {
	resp := postJSON(t, "/turns", `{
		"user_id": "u1",
		"messages": [{"role":"user","content":"hello"}],
		"timestamp": "2025-03-15T10:00:00Z",
		"metadata": {}
	}`)
	mustStatus(t, resp, 400)
}

func TestMalformed_EmptyMessages(t *testing.T) {
	resp := postJSON(t, "/turns", `{
		"session_id": "s1",
		"user_id": "u1",
		"messages": [],
		"timestamp": "2025-03-15T10:00:00Z",
		"metadata": {}
	}`)
	mustStatus(t, resp, 400)
}

func TestMalformed_OversizedBody(t *testing.T) {
	// Build valid JSON with a content field exceeding 1 MB so the body-size limiter
	// triggers while the JSON decoder is reading the large string value.
	content := strings.Repeat("x", 1024*1024)
	body := fmt.Sprintf(
		`{"session_id":"s1","user_id":"u1","messages":[{"role":"user","content":%q}],"timestamp":"2025-03-15T10:00:00Z","metadata":{}}`,
		content,
	)
	resp := postRaw(t, "/turns", "application/json", []byte(body))
	mustStatus(t, resp, 413)
}

func TestMalformed_NullBytesInContent(t *testing.T) {
	// Build the JSON unicode escape sequence at runtime from its component bytes
	// so that the Go source file itself contains no NUL character.
	// When the server's JSON decoder processes this escape it produces U+0000 in
	// the decoded string; sanitizeText strips it before the Postgres INSERT.
	jsonEscape := string([]byte{'\\', 'u', '0', '0', '0', '0'})
	body := `{"session_id":"s-nullbytes","user_id":"u-nullbytes","messages":[{"role":"user","content":"hello` +
		jsonEscape + `world"}],"timestamp":"2025-03-15T10:00:00Z","metadata":{}}`
	resp := postJSON(t, "/turns", body)
	mustStatus(t, resp, 201)

	deleteReq(t, "/users/u-nullbytes")
}

func TestMalformed_UnicodeContent(t *testing.T) {
	resp := postJSON(t, "/turns", `{
		"session_id": "s-unicode",
		"user_id": "u-unicode",
		"messages": [{"role":"user","content":"Hello World in Japanese"}],
		"timestamp": "2025-03-15T10:00:00Z",
		"metadata": {}
	}`)
	mustStatus(t, resp, 201)

	deleteReq(t, "/users/u-unicode")
}

func TestMalformed_MissingTimestamp(t *testing.T) {
	resp := postJSON(t, "/turns", `{
		"session_id": "s1",
		"user_id": "u1",
		"messages": [{"role":"user","content":"hello"}],
		"metadata": {}
	}`)
	mustStatus(t, resp, 400)
}

func TestMalformed_UnknownEndpoint(t *testing.T) {
	resp := postJSON(t, "/nonexistent", `{"foo":"bar"}`)
	mustStatus(t, resp, 404)
}

func TestMalformed_RecallMissingQuery(t *testing.T) {
	resp := postJSON(t, "/recall", `{
		"session_id": "s1",
		"user_id": "u1",
		"max_tokens": 512
	}`)
	mustStatus(t, resp, 400)
}

func TestMalformed_SearchMissingQuery(t *testing.T) {
	resp := postJSON(t, "/search", `{"limit": 5}`)
	mustStatus(t, resp, 400)
}
