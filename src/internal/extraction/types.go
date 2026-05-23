// Package extraction handles LLM-based extraction of structured memories
// from raw conversation turns.
package extraction

// Candidate is a memory candidate produced by extraction.
// In Stage 3 all candidates are inserted as ADD (no consolidation).
type Candidate struct {
	Type       string   // "fact" | "preference" | "opinion" | "event"
	Key        *string  // nil for events and keyless items
	Value      string
	Evidence   string   // "explicit" | "implicit"
	Entities   []string
	Confidence float32
}
