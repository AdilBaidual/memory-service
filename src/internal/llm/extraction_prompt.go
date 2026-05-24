// Package llm provides an OpenAI client wrapper for extraction, embeddings,
// and representation updates.
package llm

import "strings"

// TODO: tune prompt
const extractionSystemPrompt = `You are a memory extraction system for an AI assistant.
Your job is to extract structured facts, preferences, opinions, and events
from a conversation that are specifically about the USER.

Rules:
- Only extract information ABOUT THE USER (not general facts about the world).
- type field must be one of: fact, preference, opinion, event
  * fact: a current state or attribute (city, employer, pet, relationship status)
  * preference: a recurring like/dislike or behavioral pattern
  * opinion: a view, stance, or belief on a specific topic
  * event: something specific that happened at a point in time
- key field: use snake_case canonical key for facts and preferences
  (e.g. current_city, current_employer, has_pet, communication_style).
  For opinions, use a topic key (e.g. typescript_view, remote_work_view).
  For events, leave key as empty string.
- evidence field:
  * explicit: user directly and clearly stated this
  * implicit: inferred from what the user said
- entities: list ALL named entities (people, places, organizations,
  animals, named objects) that appear in this item's value field.
  This applies to ALL types including events and opinions — never
  leave entities empty if the value contains a proper noun.
  Examples: "Started at Stripe" → entities: ["Stripe"]
            "Walking Biscuit in the park" → entities: ["Biscuit"]
            "Moved to Berlin from NYC" → entities: ["Berlin", "NYC"]
- For tool-role messages: use their content as context only.
  Do NOT attribute tool outputs as user facts.
- Prefer over-extraction to under-extraction. It is better to extract
  a borderline item than to miss a real fact.
- Return an empty items array if there is truly nothing to extract.`

func buildExtractionPrompt(req ExtractionRequest) string {
	var sb strings.Builder

	if len(req.ExistingKeys) > 0 {
		sb.WriteString("EXISTING CANONICAL KEYS FOR THIS USER")
		sb.WriteString(" (use these keys when extracting the same type of information):\n")
		for _, k := range req.ExistingKeys {
			sb.WriteString("  - ")
			sb.WriteString(k)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(req.ExistingOpinionTopics) > 0 {
		sb.WriteString("EXISTING OPINION TOPICS FOR THIS USER")
		sb.WriteString(" (use these topic keys for opinions on the same subject):\n")
		for _, t := range req.ExistingOpinionTopics {
			sb.WriteString("  - ")
			sb.WriteString(t)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("CONVERSATION TO EXTRACT FROM:\n")
	sb.WriteString(req.Conversation)

	return sb.String()
}
