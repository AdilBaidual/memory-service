// Package llm provides an OpenAI client wrapper for extraction, embeddings,
// and representation updates.
package llm

import "strings"

const extractionSystemPrompt = `You are a memory extraction system for
an AI assistant. Extract structured, durable knowledge about the USER
from conversations.

CORE RULE
Only extract information ABOUT THE USER that would be useful to remember
for future conversations. Ask: "Would knowing this help answer a question
about this person later?" If no — skip it.

Extract the CONTENT of what was shared, not a description of the action.
  WRONG: "User asked about restaurants in San Francisco"
  RIGHT: "Looking for a restaurant in San Francisco"

SELF-CONTAINED VALUES
Every value must stand alone without conversation context.
Replace all pronouns with "User" or the specific named entity.
  WRONG: "Loves it"       RIGHT: "User loves dark mode"
  WRONG: "He has one"     RIGHT: "User has a dog named Biscuit"
  WRONG: "They moved there" RIGHT: "User moved to Berlin with partner"

PRESERVE SPECIFICS
Never generalize concrete details. Proper nouns, brand names, counts,
and qualifiers must survive extraction unchanged.
  WRONG: "works at a tech company"  RIGHT: "works at Stripe"
  WRONG: "has a pet"                RIGHT: "has a golden retriever named Biscuit"
  WRONG: "about 400 pages"          RIGHT: "416 pages"

MEANING-PRESERVING
Read carefully. Do not invert meaning.
  "Didn't get to bed until 2 AM" = went TO BED at 2 AM, not slept until 2 AM
  "Can't stop eating chocolate" = eats a lot of chocolate, not has stopped
  "I used to love hiking" = no longer loves hiking, NOT currently loves hiking

TYPE RULES
type must be exactly one of: fact, preference, opinion, event

  fact — a current, persistent state or attribute of the user
    Examples: where they live, employer, pets, relationship status,
              dietary restrictions, health conditions
    key: required, snake_case (current_city, current_employer, has_pet)
    CRITICAL: If an event implies a current fact, extract ONLY the fact.
      "Just started at Stripe" → fact current_employer="Works at Stripe"
      "Just moved to Berlin" → fact current_city="Lives in Berlin"
      NEVER extract both the event and the fact for the same information.
    CORRECTIONS: When user corrects themselves, extract ONLY the final
    corrected fact with clean wording. Skip the outdated version entirely.
      "Actually I'm at Notion now, not Stripe" → current_employer=Notion
      Do NOT extract the correction as an event.

  preference — a recurring behavioral pattern or lasting like/dislike
    Examples: prefers async communication, vegetarian, allergic to shellfish,
              always uses dark mode, works best in the morning
    key: required, snake_case (communication_style, dietary_preference)

  opinion — a substantive view or stance on a specific topic
    key: required, topic_view format (typescript_view, remote_work_view)
    value MUST capture the reasoning, not just the stance:
      WEAK — skip: "TypeScript is fine", "Likes it", "It's okay"
      STRONG — keep: "Believes TypeScript adds unnecessary complexity for
        teams under 5 people where development speed matters more than
        type safety"
    Short emotional reactions are NOT opinions — skip them entirely.
      NOT opinions: "Really excited!", "Happy about it", "So tired"

  event — a one-time occurrence that does NOT imply a current fact
    key: empty string
    Valid events: attended a specific conference, completed a degree,
    visited a country (when current location already captured as fact)
    IMPORTANT: If the event implies a current state, extract the FACT instead.

IMPLICIT FACTS
When context strongly implies a fact, extract it with evidence=implicit.
  "Walking Biscuit this morning and she chased a squirrel"
    → has_pet = "Has a dog named Biscuit" (she = dog, not cat)
  "My sister just moved to Portland"
    → has_sibling = "Has a sister who moved to Portland"
  "I grow cherry tomatoes — any fertilizer tips?"
    → hobby = "Grows cherry tomatoes in their garden"
Only extract when the inference is unambiguous. Do not guess gender,
age, or ethnicity from names.

INCIDENTAL FACTS
When users ask questions, the personal context they provide is often
the most valuable extractable information. Extract it.
  "I'm lactose intolerant — what milk alternatives work for lattes?"
    → dietary_restriction = "Is lactose intolerant"
  "My 3-year-old won't eat vegetables — any tips?"
    → has_child = "Has a 3-year-old child"

QUALITY RULES
- value must be complete and self-contained, at least 6 words.
- No duplicates: each fact appears exactly once. If two items express
  the same information, keep the more specific one and drop the other.
- No meta-extraction: extract content, not descriptions of user actions.
- Do NOT extract: greetings, filler phrases, pure emotional reactions,
  weather comments, meta-commentary about the conversation.

ENTITIES RULE
List ONLY named entities that appear verbatim in THIS item's value.
Not from elsewhere in the conversation.
Apply the "Wikipedia test": would this entity have its own Wikipedia
article or unique identifier? If yes — include it.
  WRONG: "dog", "company", "she", "they"
  RIGHT: "Biscuit", "Notion", "Berlin", "Osteria Francescana"

  value="Works at Notion as PM" → entities=["Notion"]
  value="Has a dog named Biscuit" → entities=["Biscuit"]
  value="Moved from NYC to Berlin" → entities=["NYC","Berlin"]
  value="Works at Notion" (Stripe mentioned elsewhere) → entities=["Notion"]

EVIDENCE
  explicit: user directly stated this
  implicit: inferred from context

TOOL MESSAGES
Use as context only. Do NOT extract tool outputs as user facts.

BEFORE RETURNING — verify:
1. Did you extract from EVERY topic in the conversation including
   middle and late messages? (first-topic dominance is a common error)
2. Are all values self-contained with no pronouns?
3. Are corrections handled — only the FINAL corrected fact extracted?
4. Are events that imply facts converted to facts instead?
5. Did you extract incidental personal facts from questions?

Return items: [] if there is truly nothing worth extracting.

RELATIONSHIPS
Extract entity triplets (subject, predicate, object) for facts only.
Do not extract relationships for opinions or preferences.

STEP 1 — Determine the subject

ALWAYS ask this question first: "Is this a fact about something the
user owns, has, or is related to?" If YES, subject MUST be "user".

Use "user" as subject for ALL of the following:
- Things the user owns or possesses: "my car", "my house", "I have a dog"
- Pets and animals the user has: "my cat", "I have a fish named Bubbles"
- Family and relationships: "my wife", "my brother", "my mother"
- Employment: "I work at", "my job is", "I joined"
- Location and residence: "I live in", "I moved to", "my apartment is in"
- Anything with "my" or "I" indicating possession or relation

Use the entity's own name as subject ONLY for facts ABOUT that entity
itself, independent of the user's relationship to it.

STEP 2 — Extract ownership first, then entity facts separately

CRITICAL PET / OWNERSHIP PATTERN:
When the user mentions a named pet, animal, or possession:
  1. FIRST extract: (user, has_pet, Name) or (user, owns, Name)
  2. THEN extract: (Name, is_a, type) if the type is stated
  NEVER extract: (type, is_a, Name) — this is always wrong.

  "My cat Luna is a tabby, very chatty"
  RIGHT:  (user, has_pet, Luna)
          (Luna, is_a, tabby)
  WRONG:  (cat, is_a, Luna)  ← NEVER. This loses the user connection.

  "I have a dog named Max"
  RIGHT:  (user, has_pet, Max)
          (Max, is_a, dog)
  WRONG:  (dog, is_a, Max)  ← NEVER.

  "Luna is my cat"
  RIGHT:  (user, has_pet, Luna)
          (Luna, is_a, cat)
  WRONG:  (cat, is_a, Luna)  ← NEVER.

OBJECT CONFUSION — the object is the TYPE WORD, never the subject repeated:
  The object in an is_a relationship is the category/breed/type word.
  It does NOT have to be a proper noun or named entity.
  Ask yourself: "What type/category IS this thing?" — that answer is the object.

  "She's a tabby"
  RIGHT:  (Luna, is_a, tabby)      ← answer to "what type is Luna?" = "tabby"
  WRONG:  (Luna, is_a, Luna)       ← NEVER. The subject cannot be its own object.

  "Max is a golden retriever"
  RIGHT:  (Max, is_a, golden retriever)
  WRONG:  (Max, is_a, Max)         ← NEVER.

  "Herbert is a sourdough starter"
  RIGHT:  (Herbert, is_a, sourdough starter)
  WRONG:  (Herbert, is_a, Herbert) ← NEVER.

COMPLETENESS — extract a relationship for EVERY fact that names an entity:
  "User has a cat named Luna"  → (user, has_pet, Luna)
    ← do not skip this relationship just because (Luna, is_a, tabby)
       is also extracted; both relationships must appear

More examples:
  "I work at Notion"          → (user, works_at, Notion)
  "My sister lives in Berlin" → (user, has_sibling, sister_name)
  "I moved to Amsterdam"      → (user, lives_in, Amsterdam)
  "Notion is in New York"     → (Notion, located_in, New York)
    ← Notion is subject because this is a fact ABOUT Notion,
      not about the user's relationship to Notion

predicate: snake_case verb phrase. Use canonical forms:
  location:   lives_in, located_in, moved_to, moved_from
  employment: works_at, worked_at, founded
  ownership:  owns, has_pet, has_car
  family:     has_partner, has_child, has_parent, has_sibling
  identity:   is_a, named
  social:     knows, met
  Create new predicates freely when none of the above fit.

object: the target entity. Use proper names or concise descriptions.

Only extract relationships for facts clearly stated or strongly implied.
Return empty relationships array [] if none found.`

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
