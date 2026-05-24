// Package tests contains component tests for the memory service HTTP API.
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Fixture data structures — defined here since tests/ is its own module
// and does not import src/internal/fixtures.

type Fixture struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Conversations []Conversation `yaml:"conversations"`
	Probes        []Probe        `yaml:"probes"`
}

type Conversation struct {
	SessionID string    `yaml:"session_id"`
	UserID    string    `yaml:"user_id"`
	Timestamp time.Time `yaml:"timestamp"`
	Messages  []FxMsg   `yaml:"messages"`
}

type FxMsg struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

type Probe struct {
	Query            string   `yaml:"query"`
	SessionID        string   `yaml:"session_id"`
	UserID           string   `yaml:"user_id"`
	MaxTokens        int      `yaml:"max_tokens"`
	ExpectedFacts    []string `yaml:"expected_facts"`
	NotExpectedFacts []string `yaml:"not_expected_facts"`
}

func loadFixtures(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read fixtures dir %s: %w", dir, err)
	}

	var fixtures []Fixture
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var f Fixture
		if err := yaml.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

func TestFixtureQuality(t *testing.T) {
	fixturesDir := os.Getenv("FIXTURES_DIR")
	if fixturesDir == "" {
		fixturesDir = "/fixtures" // path inside the test container
	}

	fixtures, err := loadFixtures(fixturesDir)
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}

	hasData := false
	for _, f := range fixtures {
		if len(f.Conversations) > 0 {
			hasData = true
			break
		}
	}
	if !hasData {
		t.Log("no fixture data yet — populate fixtures/*.yaml to enable quality measurement")
		return
	}

	totalHits, totalExpected, totalNotExpectedFails := 0, 0, 0

	for _, f := range fixtures {
		if len(f.Conversations) == 0 {
			t.Logf("[%s] no conversations, skipping", f.Name)
			continue
		}

		t.Run(f.Name, func(t *testing.T) {
			// Wipe all users from this fixture so stale data from previous
			// runs cannot pollute retrieval results.
			for _, userID := range fixtureUserIDs(f) {
				resp := deleteReq(t, "/users/"+userID)
				resp.Body.Close()
			}

			// Ingest all conversations
			for _, conv := range f.Conversations {
				msgs := "["
				for i, m := range conv.Messages {
					if i > 0 {
						msgs += ","
					}
					msgs += fmt.Sprintf(`{"role":%q,"content":%q}`, m.Role, m.Content)
				}
				msgs += "]"

				body := fmt.Sprintf(`{
					"session_id": %q,
					"user_id": %q,
					"messages": %s,
					"timestamp": %q,
					"metadata": {}
				}`, conv.SessionID, conv.UserID, msgs, conv.Timestamp.Format(time.RFC3339))

				resp := postJSON(t, "/turns", body)
				if resp.StatusCode != 201 {
					t.Errorf("ingest turn for session %s: got %d", conv.SessionID, resp.StatusCode)
				}
				resp.Body.Close()
			}

			// Run probes
			fixtureHits, fixtureExpected := 0, 0
			for _, probe := range f.Probes {
				maxTokens := probe.MaxTokens
				if maxTokens == 0 {
					maxTokens = 512
				}

				body := fmt.Sprintf(`{
					"query": %q,
					"session_id": %q,
					"user_id": %q,
					"max_tokens": %d
				}`, probe.Query, probe.SessionID, probe.UserID, maxTokens)

				resp := postJSON(t, "/recall", body)
				if resp.StatusCode != 200 {
					t.Errorf("probe %q: got status %d", probe.Query, resp.StatusCode)
					resp.Body.Close()
					continue
				}
				context := readBody(t, resp)

				// Count expected facts
				hits := 0
				for _, expected := range probe.ExpectedFacts {
					if strings.Contains(strings.ToLower(context),
						strings.ToLower(expected)) {
						hits++
					}
				}

				// Count not-expected violations
				violations := 0
				for _, notExpected := range probe.NotExpectedFacts {
					if strings.Contains(strings.ToLower(context),
						strings.ToLower(notExpected)) {
						violations++
						t.Logf("  [VIOLATION] %q found in context but should not be",
							notExpected)
					}
				}

				fixtureHits += hits
				fixtureExpected += len(probe.ExpectedFacts)
				totalNotExpectedFails += violations

				t.Logf("  probe %q: %d/%d hits",
					probe.Query, hits, len(probe.ExpectedFacts))
			}

			totalHits += fixtureHits
			totalExpected += fixtureExpected

			pct := 0.0
			if fixtureExpected > 0 {
				pct = float64(fixtureHits) / float64(fixtureExpected) * 100
			}
			t.Logf("[%s] %d/%d expected facts (%.0f%%)",
				f.Name, fixtureHits, fixtureExpected, pct)
		})
	}

	// Aggregate
	overallPct := 0.0
	if totalExpected > 0 {
		overallPct = float64(totalHits) / float64(totalExpected) * 100
	}
	t.Logf("─────────────────────────────────────────")
	t.Logf("OVERALL: %d/%d expected facts (%.0f%%)",
		totalHits, totalExpected, overallPct)
	if totalNotExpectedFails > 0 {
		t.Logf("NOT-EXPECTED violations: %d", totalNotExpectedFails)
	}
	t.Logf("─────────────────────────────────────────")

	// No assertion — this is a measurement tool, not pass/fail.
	// Copy the OVERALL line into CHANGELOG after each iteration.
}

// fixtureUserIDs returns the deduplicated set of user_ids across all
// conversations in a fixture. Used for pre-run cleanup.
func fixtureUserIDs(f Fixture) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, conv := range f.Conversations {
		if conv.UserID != "" && !seen[conv.UserID] {
			seen[conv.UserID] = true
			ids = append(ids, conv.UserID)
		}
	}
	return ids
}
