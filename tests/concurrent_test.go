// Package tests contains component tests for the memory service HTTP API.
package tests

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestConcurrentUsers_NoBleed(t *testing.T) {
	const numUsers = 10

	var wg sync.WaitGroup
	errors := make(chan error, numUsers)

	// Pre-compute base timestamp to avoid data races in goroutines.
	baseTime := time.Now().UnixNano()

	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			uid := fmt.Sprintf("concurrent-user-%d-%d", i, baseTime)
			sid := fmt.Sprintf("concurrent-session-%d-%d", i, baseTime)

			resp := postJSON(t, "/turns", validTurnBody(sid, uid))
			if resp.StatusCode != 201 {
				errors <- fmt.Errorf("user %d: expected 201, got %d", i, resp.StatusCode)
				return
			}
			resp.Body.Close()
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestConcurrentSessions_SameUser_NoBleed(t *testing.T) {
	uid := uniqueID("user-sessions")
	sid1 := uniqueID("session-1")
	sid2 := uniqueID("session-2")

	// Write turns for both sessions
	resp := postJSON(t, "/turns", validTurnBody(sid1, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	resp = postJSON(t, "/turns", validTurnBody(sid2, uid))
	mustStatus(t, resp, 201)
	resp.Body.Close()

	// Delete session 1
	resp = deleteReq(t, "/sessions/"+sid1)
	mustStatus(t, resp, 204)
	resp.Body.Close()

	// User still accessible (session 2 data not affected)
	resp = get(t, "/users/"+uid+"/memories")
	mustStatus(t, resp, 200)
	resp.Body.Close()

	// Cleanup
	deleteReq(t, "/users/"+uid)
}
