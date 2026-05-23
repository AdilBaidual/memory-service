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

func TestDeleteSessionPreservesMemories(t *testing.T) {
	// Verifies: DELETE /sessions removes turns but preserves derived memories.
	// In Stage 2 we verify the weaker form: DELETE /sessions returns 204 and does
	// not delete memories that exist independently.
	uid := uniqueID("user")
	sid := uniqueID("session")

	// Write a turn
	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)

	// Delete the session
	resp = deleteReq(t, "/sessions/"+sid)
	mustStatus(t, resp, 204)

	// Memories endpoint still works (returns empty, not error)
	resp = get(t, "/users/"+uid+"/memories")
	mustStatus(t, resp, 200)

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
