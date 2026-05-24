// Package assembler implements priority-based context assembly with token budgeting
// for the memory service recall pipeline.
package assembler

import (
	"fmt"
	"strings"
	"time"

	tiktoken "github.com/pkoukk/tiktoken-go"

	"memory-service/internal/adapters/store"
)

// AssemblyInput is everything the assembler needs to build context.
type AssemblyInput struct {
	UserID        string
	Query         string
	MaxTokens     int
	StableFacts   []store.Memory // facts + preferences, sorted by confidence DESC
	OpinionViews  []store.Memory // opinion_view memories for this user
	RetrievedMems []store.Memory // from hybrid retrieval, already ranked
	RecentEvents  []store.Memory // recent events, sorted by created_at DESC
}

// AssemblyOutput is the formatted context string plus the memories included as citations.
type AssemblyOutput struct {
	Context  string
	Included []store.Memory // memories that made it into context
}

// Assemble builds a structured markdown context string respecting the token budget.
// Sections are filled in priority order: stable facts → opinion views →
// retrieved memories → recent events.
func Assemble(input AssemblyInput) AssemblyOutput {
	maxTokens := input.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	var sections []string
	var included []store.Memory
	used := 0

	// Priority 1: Stable facts and preferences — always try first.
	if factsSection, facts := renderFactsSection(input.StableFacts); factsSection != "" {
		cost := countTokens(factsSection)
		if used+cost <= maxTokens {
			sections = append(sections, factsSection)
			included = append(included, facts...)
			used += cost
		}
	}

	// Priority 2: Synthesized opinion views.
	if opinionsSection, opinions := renderOpinionsSection(input.OpinionViews); opinionsSection != "" {
		cost := countTokens(opinionsSection)
		if used+cost <= maxTokens {
			sections = append(sections, opinionsSection)
			included = append(included, opinions...)
			used += cost
		}
	}

	// Priority 3: Query-relevant memories — add one at a time until budget exhausted.
	var relevantLines []string
	for _, mem := range input.RetrievedMems {
		line := renderMemLine(mem)
		cost := countTokens(line)
		if used+cost > maxTokens {
			break
		}
		relevantLines = append(relevantLines, line)
		included = append(included, mem)
		used += cost
	}
	if len(relevantLines) > 0 {
		sections = append(sections, renderSection("Relevant memories", relevantLines))
	}

	// Priority 4: Recent context events — fill remaining budget.
	var recentLines []string
	for _, mem := range input.RecentEvents {
		line := renderMemLine(mem)
		cost := countTokens(line)
		if used+cost > maxTokens {
			break
		}
		recentLines = append(recentLines, line)
		included = append(included, mem)
		used += cost
	}
	if len(recentLines) > 0 {
		sections = append(sections, renderSection("Recent context", recentLines))
	}

	return AssemblyOutput{
		Context:  strings.Join(sections, "\n\n"),
		Included: included,
	}
}

func renderFactsSection(mems []store.Memory) (string, []store.Memory) {
	if len(mems) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(mems))
	for _, m := range mems {
		key := m.Type
		if m.Key != nil && *m.Key != "" {
			key = *m.Key
		}
		line := fmt.Sprintf("- %s: %s", key, m.Value)
		if !m.UpdatedAt.IsZero() {
			line += fmt.Sprintf(" (updated %s)", m.UpdatedAt.Format(time.DateOnly))
		}
		lines = append(lines, line)
	}
	return renderSection("Known facts about this user", lines), mems
}

func renderOpinionsSection(mems []store.Memory) (string, []store.Memory) {
	if len(mems) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(mems))
	for _, m := range mems {
		key := "unknown_topic"
		if m.Key != nil && *m.Key != "" {
			key = *m.Key
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", key, m.Value))
	}
	return renderSection("Views and opinions", lines), mems
}

func renderMemLine(m store.Memory) string {
	ts := m.CreatedAt.Format(time.DateOnly)
	if m.Key != nil && *m.Key != "" {
		return fmt.Sprintf("- [%s] %s: %s", ts, *m.Key, m.Value)
	}
	return fmt.Sprintf("- [%s] %s", ts, m.Value)
}

func renderSection(header string, lines []string) string {
	var sb strings.Builder
	sb.WriteString("## ")
	sb.WriteString(header)
	sb.WriteString("\n")
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteString("\n")
	}
	return sb.String()
}

// countTokens returns an approximate token count for text.
// Uses tiktoken cl100k_base when available; falls back to words*1.3 otherwise.
func countTokens(text string) int {
	enc, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return int(float64(len(strings.Fields(text))) * 1.3)
	}
	return len(enc.Encode(text, nil, nil))
}
