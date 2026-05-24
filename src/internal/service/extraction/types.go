// Package extraction handles LLM-based extraction of structured memories
// from raw conversation turns.
package extraction

type Candidate struct {
	Type       string  // "fact" | "preference" | "opinion" | "event"
	Key        *string // nil for events and keyless items
	Value      string
	// Evidence is "explicit" (user stated directly) or "implicit" (inferred).
	Evidence   string
	Entities   []string
	Confidence float32
}

// ExtractionInput carries everything the extraction service needs to call the LLM.
// The caller is responsible for fetching existing key-values and opinion topics from storage.
type ExtractionInput struct {
	Conversation          string
	ExistingKeyValues     []KeyValue
	ExistingOpinionTopics []string
}

// Local type — callers do not need to import adapters/llm.
type KeyValue struct {
	Key   string
	Value string
}

type Message struct {
	Role    string
	Content string
}
