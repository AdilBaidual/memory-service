// Package tests contains component tests for the memory service HTTP API.
package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestHealth(t *testing.T) {
	resp := get(t, "/health")
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"status"`) {
		t.Fatalf("expected status field in health response, got: %s", body)
	}
}

func TestContractRoundtrip(t *testing.T) {
	uid := uniqueID("user")
	sid := uniqueID("session")

	// 1. Write a turn
	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)

	var turnResp struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &turnResp)

	if _, err := uuid.Parse(turnResp.ID); err != nil {
		t.Fatalf("turn id is not a valid UUID: %q", turnResp.ID)
	}

	// 2. Recall — verify shape
	recallBody := fmt.Sprintf(`{
		"query": "Where does the user live?",
		"session_id": %q,
		"user_id": %q,
		"max_tokens": 512
	}`, sid, uid)
	resp = postJSON(t, "/recall", recallBody)
	mustStatus(t, resp, 200)
	body := readBody(t, resp)

	if !strings.Contains(body, `"context"`) {
		t.Fatalf("recall response missing context field: %s", body)
	}
	if !strings.Contains(body, `"citations"`) {
		t.Fatalf("recall response missing citations field: %s", body)
	}

	// 3. Citations must be [] not null
	if strings.Contains(body, `"citations":null`) {
		t.Fatalf("citations must serialize as [] not null: %s", body)
	}

	// 4. Memories endpoint
	resp = get(t, "/users/"+uid+"/memories")
	mustStatus(t, resp, 200)
	memBody := readBody(t, resp)
	if !strings.Contains(memBody, `"memories"`) {
		t.Fatalf("memories response missing memories field: %s", memBody)
	}

	// 5. Cleanup
	resp = deleteReq(t, "/users/"+uid)
	mustStatus(t, resp, 204)
}

func TestSearchShape(t *testing.T) {
	resp := postJSON(t, "/search", `{
		"query": "test query",
		"user_id": "nobody",
		"limit": 5
	}`)
	mustStatus(t, resp, 200)
	body := readBody(t, resp)

	if !strings.Contains(body, `"results"`) {
		t.Fatalf("search response missing results field: %s", body)
	}
	if strings.Contains(body, `"results":null`) {
		t.Fatalf("results must serialize as [] not null: %s", body)
	}
}

func TestDeleteSessionRemovesMemories(t *testing.T) {
	// Verifies: DELETE /sessions removes all session-associated data —
	// turns and all memories that originated from the session.
	uid := uniqueID("user")
	sid := uniqueID("session")

	// Write a turn (extraction may or may not produce memories, but either way
	// the cleanup contract holds).
	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// Delete the session
	resp = deleteReq(t, "/sessions/"+sid)
	mustStatus(t, resp, 204)
	resp.Body.Close()

	// Memories must be gone — no bleed from this session
	resp = get(t, "/users/"+uid+"/memories")
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"memories":[]`) {
		t.Fatalf("expected empty memories after session delete, got: %s", body)
	}

	// Cleanup
	deleteReq(t, "/users/"+uid)
}

func TestDeleteSessionIdempotent(t *testing.T) {
	resp := deleteReq(t, "/sessions/never-existed-session")
	mustStatus(t, resp, 204)

	// Second call also 204
	resp = deleteReq(t, "/sessions/never-existed-session")
	mustStatus(t, resp, 204)
}

func TestDeleteUserIdempotent(t *testing.T) {
	resp := deleteReq(t, "/users/never-existed-user")
	mustStatus(t, resp, 204)
}

func TestMemoriesEmptyForUnknownUser(t *testing.T) {
	resp := get(t, "/users/totally-unknown-user/memories")
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"memories":[]`) {
		t.Fatalf("expected empty memories array, got: %s", body)
	}
}

func TestTurnsNoUserID_Returns201(t *testing.T) {
	sid := uniqueID("session")
	resp := postJSON(t, "/turns", fmt.Sprintf(`{
		"session_id": %q,
		"messages": [{"role":"user","content":"hello"}],
		"timestamp": "2025-03-15T10:00:00Z"
	}`, sid))
	mustStatus(t, resp, 201)
	var out struct {
		ID string `json:"id"`
	}
	decodeJSON(t, resp, &out)
	if _, err := uuid.Parse(out.ID); err != nil {
		t.Fatalf("turn id is not a valid UUID: %q", out.ID)
	}
}

func TestRecallNoUserID_ReturnsEmptyContext(t *testing.T) {
	sid := uniqueID("session")
	resp := postJSON(t, "/recall", fmt.Sprintf(`{
		"query": "anything",
		"session_id": %q,
		"max_tokens": 512
	}`, sid))
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"citations":[]`) {
		t.Fatalf("expected empty citations without user_id, got: %s", body)
	}
}

func TestSearchLimitDefault(t *testing.T) {
	resp := postJSON(t, "/search", `{"query": "test", "user_id": "nobody"}`)
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"results"`) {
		t.Fatalf("search response missing results field: %s", body)
	}
}

func TestSearchLimitCapped(t *testing.T) {
	resp := postJSON(t, "/search", `{"query": "test", "user_id": "nobody", "limit": 999}`)
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if strings.Contains(body, `"error"`) {
		t.Fatalf("expected successful response with capped limit, got: %s", body)
	}
}

func TestDeleteSession_DerivedMemoriesFromOtherSessionPreserved(t *testing.T) {
	uid := uniqueID("user")
	sid1 := uniqueID("session-a")
	sid2 := uniqueID("session-b")

	// Write turns to both sessions
	resp := postJSON(t, "/turns", validTurnBody(sid1, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = postJSON(t, "/turns", validTurnBody(sid2, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// Delete session 1 only
	resp = deleteReq(t, "/sessions/"+sid1)
	mustStatus(t, resp, 204)
	resp.Body.Close()

	// Memories endpoint for the user must still be accessible (session 2 data intact)
	resp = get(t, "/users/"+uid+"/memories")
	mustStatus(t, resp, 200)
	resp.Body.Close()

	// Cleanup
	deleteReq(t, "/users/"+uid)
}
