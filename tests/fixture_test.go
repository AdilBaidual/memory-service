// Package tests contains component tests for the memory service HTTP API.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

type JudgeAssertion struct {
	Claim        string `yaml:"claim"`
	ShouldBeTrue bool   `yaml:"should_be_true"`
}

type Probe struct {
	Query            string           `yaml:"query"`
	SessionID        string           `yaml:"session_id"`
	UserID           string           `yaml:"user_id"`
	MaxTokens        int              `yaml:"max_tokens"`
	ExpectedFacts    []string         `yaml:"expected_facts"`
	NotExpectedFacts []string         `yaml:"not_expected_facts"`
	JudgeAssertions  []JudgeAssertion `yaml:"judge_assertions"`
}

func loadFixtures(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read fixtures dir %s: %w", dir, err)
	}

	var fixtures []Fixture
	for _, e := range entries {
		if e.Name() != "02_fact_evolution.yaml" {
			continue
		}
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
	totalJudgeHits, totalJudgeTotal, totalJudgeViolations := 0, 0, 0

	for _, f := range fixtures {
		if len(f.Conversations) == 0 {
			t.Logf("[%s] no conversations, skipping", f.Name)
			continue
		}

		t.Run(f.Name, func(t *testing.T) {
			// Wipe all users from this fixture so stale data from previous
			// runs cannot pollute retrieval results.
			for _, userID := range fixtureUserIDs(f) {
				fmt.Println("DELETED userID:", userID)
				resp := deleteReq(t, "/users/"+userID)
				mustStatus(t, resp, 204)
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
				recallCtx := readBody(t, resp)

				// Count expected facts
				hits := 0
				for _, expected := range probe.ExpectedFacts {
					if strings.Contains(strings.ToLower(recallCtx),
						strings.ToLower(expected)) {
						hits++
					}
				}

				// Count not-expected violations
				violations := 0
				for _, notExpected := range probe.NotExpectedFacts {
					if strings.Contains(strings.ToLower(recallCtx),
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

				// LLM judge assertions (only when judge_assertions is populated)
				judgeHits, judgeTotal, judgeViolations := 0, 0, 0
				if len(probe.JudgeAssertions) > 0 {
					judgeCtx, cancel := context.WithTimeout(
						context.Background(), 30*time.Second)
					defer cancel()

					for _, assertion := range probe.JudgeAssertions {
						verdict, err := judgeAssertion(judgeCtx, recallCtx, assertion.Claim)
						if err != nil {
							t.Logf("    [JUDGE ERROR] claim %q: %v", assertion.Claim, err)
							continue
						}

						judgeTotal++
						correct := verdict == assertion.ShouldBeTrue
						if correct {
							judgeHits++
							t.Logf("    [JUDGE PASS] claim=%q expected=%v got=%v",
								assertion.Claim, assertion.ShouldBeTrue, verdict)
						} else {
							judgeViolations++
							t.Logf("    [JUDGE FAIL] claim=%q expected=%v got=%v",
								assertion.Claim, assertion.ShouldBeTrue, verdict)
						}
					}

					if judgeTotal > 0 {
						t.Logf("    judge: %d/%d assertions correct, %d failures",
							judgeHits, judgeTotal, judgeViolations)
					}
				}

				totalJudgeHits += judgeHits
				totalJudgeTotal += judgeTotal
				totalJudgeViolations += judgeViolations
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
	if totalJudgeTotal > 0 {
		judgePct := float64(totalJudgeHits) / float64(totalJudgeTotal) * 100
		t.Logf("JUDGE: %d/%d assertions correct (%.0f%%)",
			totalJudgeHits, totalJudgeTotal, judgePct)
		if totalJudgeViolations > 0 {
			t.Logf("JUDGE violations: %d", totalJudgeViolations)
		}
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

// judgeAssertion asks gpt-4o-mini whether the claim is true given the context.
// Returns (verdict bool, err error) where verdict=true means the context supports
// the claim. Returns an error (and skips the assertion) when OPENAI_API_KEY is unset.
func judgeAssertion(ctx context.Context, recallCtx, claim string) (bool, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return false, fmt.Errorf("OPENAI_API_KEY not set")
	}

	reqBody := map[string]any{
		"model": "gpt-4o-mini",
		"messages": []map[string]string{
			{
				"role": "system",
				"content": `You are a fact-checking judge. Given a context passage and a claim, determine if the context supports the claim as true.

Rules:
- Answer only "true" or "false"
- "true" means the context explicitly states or clearly implies the claim
- "false" means the context contradicts the claim, or does not contain enough information to support it
- Focus on the CURRENT state described in the context, not historical facts
- Be strict: if the context says "previously at Stripe, now at Notion" then "user works at Stripe" is FALSE`,
			},
			{
				"role":    "user",
				"content": fmt.Sprintf("CONTEXT:\n%s\n\nCLAIM: %s\n\nIs this claim true or false?", recallCtx, claim),
			},
		},
		"max_tokens":  10,
		"temperature": 0,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return false, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Choices) == 0 {
		return false, fmt.Errorf("empty choices in response")
	}

	answer := strings.ToLower(strings.TrimSpace(result.Choices[0].Message.Content))
	return strings.HasPrefix(answer, "true"), nil
}
