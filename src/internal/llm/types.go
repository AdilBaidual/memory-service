// Package llm provides an OpenAI client wrapper for extraction, embeddings,
// and representation updates.
package llm

// ExtractionRequest is the input to the extraction LLM call.
type ExtractionRequest struct {
	// Conversation is the serialized conversation text, formatted as role: content lines.
	Conversation string
	// ExistingKeys are canonical memory keys for this user, passed as LLM hints.
	ExistingKeys []string
	// ExistingOpinionTopics are opinion topic keys for this user, passed as LLM hints.
	ExistingOpinionTopics []string
}

// Relationship is an entity triplet extracted from a conversation.
type Relationship struct {
	Subject   string // e.g. "user", "Luna", "Notion"
	Predicate string // e.g. "lives_in", "has_pet", "works_at"
	Object    string // e.g. "Amsterdam", "Luna", "Notion"
}

// ExtractionResult is the parsed output of the extraction LLM call.
type ExtractionResult struct {
	Items         []ExtractedItem
	Relationships []Relationship
}

// ExtractedItem is a single memory candidate returned by extraction.
type ExtractedItem struct {
	// Type is one of: fact, preference, opinion, event
	Type string
	// Key is the normalized snake_case key. Empty for events and some opinions.
	Key string
	// Value is the extracted text content.
	Value string
	// Evidence is "explicit" (user stated directly) or "implicit" (inferred).
	Evidence string
	// Entities is a list of named entities mentioned in this item.
	Entities []string
}
