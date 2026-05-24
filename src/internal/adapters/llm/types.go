package llm

type KeyValue struct {
	Key   string
	Value string
}

type ExtractionRequest struct {
	// Conversation is the serialized conversation text, formatted as role: content lines.
	Conversation string
	// ExistingKeyValues are canonical memory keys with their current values for this user.
	// Showing both key and current value prevents the LLM from reusing a key for a
	// semantically different piece of information (e.g. reusing has_pet for a sourdough
	// starter when the current value is already "User has a cat named Luna").
	ExistingKeyValues []KeyValue
	ExistingOpinionTopics []string
}

// Relationship is an entity triplet extracted from a conversation.
type Relationship struct {
	Subject   string // e.g. "user", "Luna", "Notion"
	Predicate string // e.g. "lives_in", "has_pet", "works_at"
	Object    string // e.g. "Amsterdam", "Luna", "Notion"
}

type ExtractionResult struct {
	Items         []ExtractedItem
	Relationships []Relationship
}

type ExtractedItem struct {
	Type     string
	Key      string
	Value    string
	// Evidence is "explicit" (user stated directly) or "implicit" (inferred).
	Evidence string
	Entities []string
}
