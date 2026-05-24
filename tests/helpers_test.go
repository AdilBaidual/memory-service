// Package tests contains component tests for the memory service HTTP API.
// Tests communicate with a running service via HTTP only — no Go internals imported.
package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// serviceURL returns the base URL of the running service.
// Reads SERVICE_URL env var, defaults to localhost for local runs.
func serviceURL() string {
	if u := os.Getenv("SERVICE_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8080"
}

// get performs a GET request and returns the response.
func get(t *testing.T, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(serviceURL() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// postJSON performs a POST with a JSON body and returns the response.
func postJSON(t *testing.T, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(
		serviceURL()+path,
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// postRaw performs a POST with arbitrary body and content type.
func postRaw(t *testing.T, path, contentType string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(serviceURL()+path, contentType, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// deleteReq performs a DELETE request.
func deleteReq(t *testing.T, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, serviceURL()+path, nil)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

// mustStatus asserts the response has the expected status code.
func mustStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d. body: %s",
			expected, resp.StatusCode, string(body))
	}
}

// readBody reads and returns the response body as string.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// decodeJSON decodes the response body into v.
func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

// uniqueID returns a unique string suffix for test isolation.
func uniqueID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// validTurnBody builds a minimal valid POST /turns request body.
func validTurnBody(sessionID, userID string) string {
	return fmt.Sprintf(`{
		"session_id": %q,
		"user_id": %q,
		"messages": [
			{"role": "user", "content": "Hello, I live in Berlin"},
			{"role": "assistant", "content": "Nice!"}
		],
		"timestamp": "2025-03-15T10:00:00Z",
		"metadata": {}
	}`, sessionID, userID)
}

// anonymousTurnBody builds a POST /turns body without user_id (session-scoped).
func anonymousTurnBody(sessionID, content string) string {
	return fmt.Sprintf(`{
		"session_id": %q,
		"messages": [
			{"role": "user", "content": %q},
			{"role": "assistant", "content": "Got it!"}
		],
		"timestamp": "2025-03-15T10:00:00Z",
		"metadata": {}
	}`, sessionID, content)
}
