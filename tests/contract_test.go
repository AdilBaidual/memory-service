// Package tests contains component tests for the memory service HTTP API.
package tests

import (
	"encoding/json"
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

// ─── POST /search ──────────────────────────────────────────────────────────────

func TestSearch_BothNullIDs_ReturnsEmpty(t *testing.T) {
	resp := postJSON(t, "/search", `{"query":"test","limit":5}`)
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if strings.Contains(body, `"results":null`) {
		t.Fatal("results must not be null")
	}
	if !strings.Contains(body, `"results":[]`) {
		t.Fatalf("expected empty results array, got: %s", body)
	}
}

func TestSearch_UnknownUser_ReturnsEmpty(t *testing.T) {
	resp := postJSON(t, "/search",
		`{"query":"test","user_id":"nobody-exists-ever","limit":5}`)
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if strings.Contains(body, `"results":null`) {
		t.Fatal("results must not be null for unknown user")
	}
}

func TestSearch_SessionOnly_ReturnsResults(t *testing.T) {
	uid := uniqueID("user")
	sid := uniqueID("session")

	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	body := fmt.Sprintf(`{"query":"hello","session_id":%q,"limit":5}`, sid)
	resp = postJSON(t, "/search", body)
	mustStatus(t, resp, 200)

	rb := readBody(t, resp)
	if strings.Contains(rb, `"results":null`) {
		t.Fatal("results must not be null for session-only search")
	}

	deleteReq(t, "/users/"+uid)
}

func TestSearch_LimitRespected(t *testing.T) {
	uid := uniqueID("user")
	for i := 0; i < 5; i++ {
		sid := uniqueID("session")
		resp := postJSON(t, "/turns", validTurnBody(sid, uid))
		mustStatus(t, resp, 201)
		resp.Body.Close()
	}

	body := fmt.Sprintf(`{"query":"hello","user_id":%q,"limit":2}`, uid)
	resp := postJSON(t, "/search", body)
	mustStatus(t, resp, 200)

	var result struct {
		Results []json.RawMessage `json:"results"`
	}
	decodeJSON(t, resp, &result)
	if len(result.Results) > 2 {
		t.Fatalf("limit=2 but got %d results", len(result.Results))
	}

	deleteReq(t, "/users/"+uid)
}

func TestSearch_ResultShape(t *testing.T) {
	uid := uniqueID("user")
	sid := uniqueID("session")

	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	body := fmt.Sprintf(`{"query":"hello","user_id":%q,"limit":5}`, uid)
	resp = postJSON(t, "/search", body)
	mustStatus(t, resp, 200)

	var result struct {
		Results []struct {
			Content   string          `json:"content"`
			Score     float64         `json:"score"`
			SessionID string          `json:"session_id"`
			Timestamp string          `json:"timestamp"`
			Metadata  json.RawMessage `json:"metadata"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("search result shape invalid: %v", err)
	}
	resp.Body.Close()

	deleteReq(t, "/users/"+uid)
}

func TestSearch_MissingQuery_Returns400(t *testing.T) {
	resp := postJSON(t, "/search", `{"user_id":"somebody","limit":5}`)
	mustStatus(t, resp, 400)
	resp.Body.Close()
}

// ─── GET /users/{id}/memories ─────────────────────────────────────────────────

func TestMemories_UnknownUser_ReturnsEmpty(t *testing.T) {
	resp := get(t, "/users/nobody-exists-ever/memories")
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if strings.Contains(body, `"memories":null`) {
		t.Fatal("memories must not be null for unknown user")
	}
	if !strings.Contains(body, `"memories":[]`) {
		t.Fatalf("expected empty memories array, got: %s", body)
	}
}

func TestMemories_MemoriesNeverNull(t *testing.T) {
	resp := get(t, "/users/nobody/memories")
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if strings.Contains(body, `"memories":null`) {
		t.Fatal("memories must serialize as [] not null")
	}
}

func TestMemories_ShowsSupersededHistory(t *testing.T) {
	uid := uniqueID("user")

	resp := postJSON(t, "/turns", fmt.Sprintf(`{
		"session_id":"s1-%s","user_id":%q,
		"messages":[{"role":"user","content":"I work at Stripe"}],
		"timestamp":"2025-01-01T10:00:00Z","metadata":{}}`, uid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = postJSON(t, "/turns", fmt.Sprintf(`{
		"session_id":"s2-%s","user_id":%q,
		"messages":[{"role":"user","content":"I switched jobs, now working at Notion"}],
		"timestamp":"2025-03-01T10:00:00Z","metadata":{}}`, uid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// active=false filter must not error and must return valid shape
	resp = get(t, "/users/"+uid+"/memories?active=false")
	mustStatus(t, resp, 200)
	body := readBody(t, resp)
	if strings.Contains(body, `"memories":null`) {
		t.Fatal("memories must not be null with active=false filter")
	}

	deleteReq(t, "/users/"+uid)
}

func TestMemories_FilterByActiveTrue(t *testing.T) {
	uid := uniqueID("user")
	sid := uniqueID("session")

	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = get(t, "/users/"+uid+"/memories?active=true")
	mustStatus(t, resp, 200)

	var result struct {
		Memories []struct {
			Active bool `json:"active"`
		} `json:"memories"`
	}
	decodeJSON(t, resp, &result)

	for _, m := range result.Memories {
		if !m.Active {
			t.Fatal("active=true filter returned inactive memory")
		}
	}

	deleteReq(t, "/users/"+uid)
}

func TestMemories_FilterByType(t *testing.T) {
	uid := uniqueID("user")
	sid := uniqueID("session")

	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = get(t, "/users/"+uid+"/memories?type=fact")
	mustStatus(t, resp, 200)

	var result struct {
		Memories []struct {
			Type string `json:"type"`
		} `json:"memories"`
	}
	decodeJSON(t, resp, &result)

	for _, m := range result.Memories {
		if m.Type != "fact" {
			t.Fatalf("type=fact filter returned memory of type %q", m.Type)
		}
	}

	deleteReq(t, "/users/"+uid)
}

func TestMemories_Pagination(t *testing.T) {
	uid := uniqueID("user")
	for i := 0; i < 3; i++ {
		sid := uniqueID("session")
		resp := postJSON(t, "/turns", validTurnBody(sid, uid))
		mustStatus(t, resp, 201)
		resp.Body.Close()
	}

	page1 := get(t, "/users/"+uid+"/memories?limit=1&offset=0")
	mustStatus(t, page1, 200)
	page2 := get(t, "/users/"+uid+"/memories?limit=1&offset=1")
	mustStatus(t, page2, 200)

	var r1, r2 struct {
		Memories []struct {
			ID string `json:"id"`
		} `json:"memories"`
	}
	decodeJSON(t, page1, &r1)
	decodeJSON(t, page2, &r2)

	if len(r1.Memories) > 0 && len(r2.Memories) > 0 {
		if r1.Memories[0].ID == r2.Memories[0].ID {
			t.Fatal("pagination returned same memory on different pages")
		}
	}

	deleteReq(t, "/users/"+uid)
}

// ─── Anonymous session (session-scoped, no user_id) ──────────────────────────

func TestAnonymousTurn_RecallShape(t *testing.T) {
	sid := uniqueID("anon-session")

	resp := postJSON(t, "/turns", anonymousTurnBody(sid, "I am a software engineer who loves Go"))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	body := fmt.Sprintf(`{"query":"what does the user do?","session_id":%q,"max_tokens":512}`, sid)
	resp = postJSON(t, "/recall", body)
	mustStatus(t, resp, 200)

	var out struct {
		Context   string          `json:"context"`
		Citations []json.RawMessage `json:"citations"`
	}
	decodeJSON(t, resp, &out)
	if out.Citations == nil {
		t.Fatal("citations must be [] not null for anonymous recall")
	}
}

func TestAnonymousTurn_SearchShape(t *testing.T) {
	sid := uniqueID("anon-search")

	resp := postJSON(t, "/turns", anonymousTurnBody(sid, "My favourite language is Rust"))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	body := fmt.Sprintf(`{"query":"favourite language","session_id":%q,"limit":5}`, sid)
	resp = postJSON(t, "/search", body)
	mustStatus(t, resp, 200)

	var out struct {
		Results []json.RawMessage `json:"results"`
	}
	decodeJSON(t, resp, &out)
	if out.Results == nil {
		t.Fatal("results must be [] not null for anonymous search")
	}
}

func TestAnonymousSessionIsolation_NoBleed(t *testing.T) {
	sidA := uniqueID("anon-a")
	sidB := uniqueID("anon-b")

	// Write distinct facts into two separate anonymous sessions.
	resp := postJSON(t, "/turns", anonymousTurnBody(sidA, "I am a baker who makes croissants"))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = postJSON(t, "/turns", anonymousTurnBody(sidB, "I am an astronaut who likes space"))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// Recall from session A must not return session B data.
	recallA := fmt.Sprintf(`{"query":"what do I do?","session_id":%q,"max_tokens":512}`, sidA)
	resp = postJSON(t, "/recall", recallA)
	mustStatus(t, resp, 200)
	bodyA := readBody(t, resp)
	if strings.Contains(strings.ToLower(bodyA), "astronaut") {
		t.Fatalf("session A recall contains session B data ('astronaut'): %s", bodyA)
	}

	// Recall from session B must not return session A data.
	recallB := fmt.Sprintf(`{"query":"what do I do?","session_id":%q,"max_tokens":512}`, sidB)
	resp = postJSON(t, "/recall", recallB)
	mustStatus(t, resp, 200)
	bodyB := readBody(t, resp)
	if strings.Contains(strings.ToLower(bodyB), "baker") {
		t.Fatalf("session B recall contains session A data ('baker'): %s", bodyB)
	}
}

func TestDeleteSession_AnonymousSession_Cleanup(t *testing.T) {
	sid := uniqueID("anon-del")

	resp := postJSON(t, "/turns", anonymousTurnBody(sid, "I live in Oslo"))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// DELETE /sessions/{id} must succeed for anonymous sessions.
	resp = deleteReq(t, "/sessions/"+sid)
	mustStatus(t, resp, 204)
	resp.Body.Close()

	// Second delete is idempotent.
	resp = deleteReq(t, "/sessions/"+sid)
	mustStatus(t, resp, 204)
	resp.Body.Close()
}

func TestAuthenticatedUser_UnchangedAfterAnonymousSessions(t *testing.T) {
	uid := uniqueID("auth-user")
	sid := uniqueID("auth-session")
	anonSid := uniqueID("anon-coexist")

	// Write an anonymous session first.
	resp := postJSON(t, "/turns", anonymousTurnBody(anonSid, "I am anonymous and love hiking"))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// Write an authenticated turn.
	resp = postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// Authenticated recall returns correct shape.
	body := fmt.Sprintf(`{"query":"where does the user live?","session_id":%q,"user_id":%q,"max_tokens":512}`, sid, uid)
	resp = postJSON(t, "/recall", body)
	mustStatus(t, resp, 200)

	var out struct {
		Context   string          `json:"context"`
		Citations []json.RawMessage `json:"citations"`
	}
	decodeJSON(t, resp, &out)
	if out.Citations == nil {
		t.Fatal("citations must not be null for authenticated recall")
	}

	// Anonymous data must not appear in authenticated user's context.
	if strings.Contains(strings.ToLower(out.Context), "anonymous") {
		t.Fatalf("authenticated recall contains anonymous session data: %s", out.Context)
	}

	deleteReq(t, "/users/"+uid)
}

func TestAnonymousRecall_EmptyForUnknownSession(t *testing.T) {
	sid := "session-that-never-existed-anon"
	body := fmt.Sprintf(`{"query":"anything","session_id":%q,"max_tokens":512}`, sid)
	resp := postJSON(t, "/recall", body)
	mustStatus(t, resp, 200)

	var out struct {
		Context   string          `json:"context"`
		Citations []json.RawMessage `json:"citations"`
	}
	decodeJSON(t, resp, &out)
	if out.Citations == nil {
		t.Fatal("citations must be [] not null for unknown anonymous session")
	}
}

func TestMemories_ResponseShape(t *testing.T) {
	uid := uniqueID("user")
	sid := uniqueID("session")

	resp := postJSON(t, "/turns", validTurnBody(sid, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = get(t, "/users/"+uid+"/memories")
	mustStatus(t, resp, 200)

	var result struct {
		Memories []struct {
			ID            string  `json:"id"`
			Type          string  `json:"type"`
			Key           *string `json:"key"`
			Value         string  `json:"value"`
			Confidence    float64 `json:"confidence"`
			SourceSession string  `json:"source_session"`
			SourceTurn    *string `json:"source_turn"`
			CreatedAt     string  `json:"created_at"`
			UpdatedAt     string  `json:"updated_at"`
			Supersedes    *string `json:"supersedes"`
			Active        bool    `json:"active"`
		} `json:"memories"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("memories response shape invalid: %v", err)
	}
	resp.Body.Close()

	if result.Limit == 0 {
		t.Fatal("limit field missing or zero in response")
	}

	deleteReq(t, "/users/"+uid)
}
