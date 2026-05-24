# Changelog

Architectural decisions and significant changes during development.
Entries are in reverse chronological order.

---

## v1.3.2 — Graph scoring precision and extraction stability

- Adds mention_count as an entity importance signal in hop1 scoring. hop1_memories now LEFT JOINs the entities table and multiplies the base score of 1.0 by `LEAST(1 + ln(mention_count) × 0.1, 2.0)` — an entity mentioned 15 times receives a ~27% score boost over a one-off name before RRF fusion. The boost is applied only at hop1; hop2 intentionally stays flat at 0.5 because the traversal-neighbour entity is almost always "user", which carries an extremely high mention_count and would uniformly inflate scores for unrelated memories if boosted.
- Fixes mention_count not decrementing when a session is deleted. entity_mentions cascade-deletes automatically with memories via FK, so the per-entity loss must be computed while the rows are still present. DeleteSessionData now issues `UPDATE entities SET mention_count = GREATEST(0, mention_count - N)` grouped by entity before the memory DELETE. DeleteAllUserData is unaffected — it removes the entities rows entirely.
- Fixes LLM extraction running at default temperature (~1.0). ChatCompletionRequest.Temperature carries `omitempty` in the go-openai struct, so a literal zero is silently dropped from the JSON body and the API falls back to its default. Sets Temperature to `math.SmallestNonzeroFloat32` (≈1.4e-45), which serialises as a non-zero value but is indistinguishable from 0 for the model. Eliminates the main source of run-to-run variance in extracted facts and relationship triplets.
- Fixes relationship triplets being anchored to the first memory that mentioned an entity in LLM output order rather than the most authoritative one. If the LLM listed a move-event before the current-city fact, `(user, lives_in, amsterdam)` was anchored to the event memory; hop2 then surfaced the event instead of the stable fact, and the Amsterdam memory remained unreachable. entityMemoryMap now tracks whether each anchor came from a fact/preference or an event/opinion. A fact/preference is allowed to override an event/opinion anchor for the same entity name regardless of output order; an event/opinion cannot override a fact/preference.
- Adds ingestion timing and recall timing to the fixture test runner. Each fixture logs total ingest time and average per-turn latency after all sessions are submitted; each probe logs its /recall round-trip time alongside the hit count.

Fixture delta vs v1.3.1:

    basic_facts:      3/3 (100%) — unchanged
    fact_evolution:   1/1 (100%) — unchanged; 1 not-expected violation (Stripe
                                   in context — bi-temporal history, expected)
    multi_hop:        1/1 (100%) — unchanged score; retrieval now stable under
                                   LLM variance (temperature fix + fact-priority
                                   anchor eliminate the flakiness observed in v1.3.1)
    noise_resistance: 0 violations — unchanged
    opinion_arc:      1/1 (100%) — unchanged; 1 not-expected violation
                                   (game changer — opinion_view not yet
                                   implemented, expected)
    OVERALL:          6/6 (100%), 2 not-expected violations
    JUDGE:            18/20 assertions correct (90%); 2 failures — same as v1.3.1,
                      both require opinion_view synthesis not yet implemented
                      ("view has evolved over time", "Luna's location determinable
                      from home city")

---

## v1.3.1 — Graph traversal correctness and extraction stability

- Fixes hop1_entities CTE filtering entity_relationships by `m.active = true` on the source memory. When a memory was superseded, its edges disappeared from traversal even though entity relationships are append-only navigation metadata — not versioned facts. Removed the JOIN to memories from hop1_entities; only hop1_memories and hop2_memories, which return actual memory content, retain the `active = true` guard.
- Fixes relationship triplets being anchored to the last inserted memory (lastMemoryID). In sessions with multiple extracted facts, a triplet like `(user, lives_in, Amsterdam)` could be anchored to a "moved from Rotterdam" event, leaving the Amsterdam memory with no entity_mentions entry and unreachable by hop2. Now each triplet resolves its source memory by looking up the object entity in a per-turn entity→memoryID map, then the subject entity, then falls back to lastMemoryID.
- Fixes inconsistent entity name casing between storage and query. The LLM sometimes returns "user" and sometimes "User" as a relationship subject; entity_mentions stored names as-is, causing case-sensitive lookups to miss matches. Entity names are now normalised to lowercase at write time in entity_relationships and entity_mentions, and extractQueryEntities returns lowercase to match.
- Fixes existing-keys hint passing only key names to the LLM. Two failure modes resulted: noise travel sessions were extracted under `current_city` — a key already in use — superseding the user's permanent location; a sourdough starter was stored under `has_pet`, superseding the cat. The hint now includes the current active value alongside each key, giving the model enough context to reject a travel episode as a `current_city` update. The hint also lists semantic-type constraints: has_pet is for animals only, current_city only changes on an explicit permanent move, current_employer only changes when the user actively works somewhere new.
- Adds TEMPORARY vs PERMANENT STATE and DEPARTURES AND ENDINGS rules to the extraction prompt. Temporary situations (travel, visits, layovers) are events; departures ("left Stripe", "quit X") are events or skipped — never a replacement for the current fact. When a user both leaves one place and joins another in the same message, only the new current state is extracted as a fact.
- Fixes relationship subject inversion for pet/ownership sentences: LLM was extracting (cat, is_a, Luna) instead of (user, has_pet, Luna). Added explicit two-step extraction pattern with WRONG/RIGHT examples plus OBJECT CONFUSION rule (object of is_a must be the type word, not the subject repeated). Added tautological triplet filter in Go: any (X, P, X) relationship is silently dropped as a model error.

Fixture delta vs v1.3.0:

    basic_facts:      3/3 (100%) — unchanged
    fact_evolution:   1/1 (100%) — unchanged; 1 not-expected violation (Stripe
                                   in context — bi-temporal history, expected)
    multi_hop:        1/1 (100%) — up from 0/1; Luna and Amsterdam now in
                                   top-2 recall positions, displacing noise
    noise_resistance: 0 violations — unchanged
    opinion_arc:      1/1 (100%) — unchanged; 1 not-expected violation
                                   (game changer — opinion_view not yet
                                   implemented, expected)
    OVERALL:          6/6 (100%), 2 not-expected violations
    JUDGE:            18/20 assertions correct (90%); 2 failures — both are
                      reasoning-chain assertions ("view has evolved over time",
                      "Luna's location determinable from home city") that
                      require opinion_view synthesis, not implemented yet

---

## v1.3.0 — Entity graph extraction and 2-hop traversal

- LLM extraction prompt extended to return relationship triplets (subject, predicate, object) alongside items
- Triplets persist to entity_relationships (append-only); entities upserted in entities; entity_mentions links each memory to the entities it references
- Third retrieval channel: 2-hop CTE — hop 1 fetches memories directly linked to query entities via entity_mentions (score 1.0), hop 2 follows entity_relationships edges to neighbour entities (score 0.5); fused into existing semantic + FTS RRF pipeline
- Query entities extracted by capitalized-word heuristic; predicate synonym groups in internal/predicates/groups.go
- Relationship writes are non-fatal — a write error logs a warning without rolling back the memory transaction


Fixture delta vs v1.2.0:

    basic_facts:      3/3 (100%) — unchanged
    fact_evolution:   1/1 (100%) — unchanged
    multi_hop:        0/1  (0%)  — entity graph now correctly populated:
                                   (user, has_pet, Luna) and (user, lives_in,
                                   Amsterdam) confirmed in entity_relationships.
                                   Fixture still fails because noise travel
                                   sessions supersede the Amsterdam memory
                                   (active=false) before the graph probe runs;
                                   the consolidation layer, not the graph, is
                                   the blocking issue.
    noise_resistance: 0 violations — unchanged
    opinion_arc:      1/1 (100%) — unchanged
    OVERALL:          5/6 (83%), 2 violations

---

## v1.2.0 — Extraction prompt overhaul

Rewrites the extraction system prompt with explicit rules for self-contained
values (all pronouns replaced with "User" or the named entity), specificity
preservation (proper nouns and counts must survive unchanged), and
meaning-preservation guards against semantic inversion. Adds dedicated sections
for implicit facts (context-inferred attributes), incidental facts (personal
context embedded in questions), and opinion quality filtering (weak stances
skipped; value must capture the reasoning). Corrections now produce only the
final corrected fact — the outdated version is not extracted. Events that imply
a current state are converted to facts rather than duplicated. A pre-output
checklist instructs the model to re-scan for missed topics before returning.

Fixture results after prompt change:

    basic_facts:      3/3 (100%)
    fact_evolution:   1/1 (100%), 1 violation — "Stripe" persists (bi-temporal history, expected)
    multi_hop:        0/1 (0%) — graph channel not yet implemented
    noise_resistance: 0 violations
    opinion_arc:      1/1 (100%), 1 violation — "game changer" present (opinion_view not yet implemented)
    OVERALL:          5/6 (83%), 2 violations
    JUDGE:            5/6 assertions correct (83%), 1 failure — opinion evolution arc not yet captured

Results are identical to v1.1.4. The prompt changes are quality improvements
for edge cases not covered by the current fixture set; the measurable delta will
surface when opinion_view and graph traversal are in place.

---

## v1.1.5 — LLM-as-judge assertions in fixture runner

Adds `judge_assertions` to fixture probes and a `judgeAssertion` function to the test runner.

Substring matching cannot distinguish "currently works at Stripe" from "previously worked at Stripe" — both contain "Stripe". The LLM judge evaluates semantic meaning rather than substrings, enabling precise assertions about current vs. historical state.

---

## v1.1.4 — Opinion accumulation and fixture hardening

Opinions and events now always insert as independent active rows (`supersedes=nil`).
Previously routed through `ConsolidateFact`, each new opinion on the same topic
superseded the prior one — collapsing the evolution arc to a single row.
Facts and preferences retain the consolidation path unchanged.

The multi_hop fixture noise was also hardened. The original 20 noise sessions
were lifestyle facts (hobbies, habits) with no city or evening content, so the
Amsterdam memory had no semantic competition and surfaced in top-10 via cosine
alone — graph traversal was never exercised. Six noise sessions were replaced
with travel episodes that explicitly mention evenings in other European cities
(Berlin, Copenhagen, Lisbon, Vienna, Warsaw, Prague). These rank above Amsterdam
for any evening-location probe because the Amsterdam memory contains no "evening"
signal. The scenario now correctly fails without the entity graph channel.

Fixture results:

    basic_facts:      3/3 (100%)
    fact_evolution:   1/1 (100%), 1 violation — "Stripe" persists
    multi_hop:        0/1 (0%) — correctly fails; graph channel not yet implemented
    noise_resistance: 0 violations
    opinion_arc:      1/1 (100%), 1 violation — "game changer" present
    OVERALL:          5/6 (83%), 2 violations

The multi_hop result dropped from the 1/1 reported in v1.1.3 — that was a false
pass caused by insufficient noise. The opinion_arc violation returned for the
same reason described under the previous entry: it was an artifact of data loss
under consolidation, not a retrieval improvement. Both violations are expected
at this retrieval configuration and will be resolved in later stages.

---

## v1.1.3 — Keyword channel recall

Switches FTS queries from AND to OR semantics. `plainto_tsquery` produces a
strict AND query; a natural-language probe like "What does the user think about
TypeScript?" fails any memory missing even one token. The fix rewrites the query
using a CTE over `to_tsvector` lexemes joined with `|`, matching any document
that shares at least one key term. Schema unchanged.

---

## v1.1.2 — Session deletion scope

DELETE /sessions now removes associated memories and cascaded entity graph edges
in addition to turns. Previously, memories were retained on session delete, causing
cross-session fact bleed when user IDs were reused across runs. Deleting
`memories WHERE source_session = $1` is sufficient — FK cascades handle
`entity_mentions` and `entity_relationships` automatically. The `entities` table
is preserved: entities are user-scoped and may span multiple sessions.

---

## v1.1.1 — FTS config: simple → english

- Switched value_tsv generated column from to_tsvector('simple', ...) to to_tsvector('english', ...)
- Updated keyword channel queries to match: plainto_tsquery('english', ...)
- 'english' adds stemming (working → work, employer → employ) and stopword removal
- multi_hop recovered: 0/1 (0%) → 1/1 (100%) — stemming lets the keyword channel reach the cat/location fact
- basic_facts regression persists: 2/3 — pet probe still below top-10 cutoff
- OVERALL: 4/6 (67%), 1 violation → 5/6 (83%), 1 violation

---

## v1.1.0 — Consolidation and hybrid retrieval

- Consolidation check (ADD / NOOP / UPDATE) runs before every memory insert
- Contradicting fact: old row marked inactive (active=false, valid_to=NOW()), new row inserted with supersedes pointer
- Matching value: updated_at touched (NOOP) — no duplicate row created
- Events and keyless items always ADD — nothing to consolidate against
- FTS channel added: Postgres full-text search on value_tsv via plainto_tsquery('simple')
- Semantic and FTS channels run concurrently; results fused via Reciprocal Rank Fusion (k=60)
- Each channel fetches up to 30 candidates before fusion; top-10 returned to caller

Fixture results:

    basic_facts:      2/3 (67%) — pet probe drops below RRF top-10 when FTS finds no match
    fact_evolution:   1/1 (100%), 1 violation — "Stripe" persists; the transition event inserts as ADD, not a supersede
    multi_hop:        0/1 (0%) — graph channel not yet implemented
    noise_resistance: 0 violations
    opinion_arc:      1/1 (100%), 0 violations
    OVERALL:          4/6 (67%), 1 violation

---

## v1.0.0 — Baseline extraction and semantic retrieval

- LLM extraction on POST /turns via gpt-4o-mini with Structured Output (Strict mode)
- Existing canonical keys passed as hints to stabilise vocabulary across turns
- Confidence computed from evidence field (explicit → 0.95, implicit → 0.70)
- Tool-role messages used as context only; facts not attributed to tool outputs
- Cosine top-k retrieval via pgvector ORDER BY <=> LIMIT; no FTS, graph, or reranker
- Context assembled as plain key: value concatenation
- All extracted facts INSERT unconditionally (no consolidation check)
- Graceful degradation when OPENAI_API_KEY is absent: turn saved, /recall returns empty

Fixture baseline metrics (after cleanup fix — each run starts from empty state):

    basic_facts:      3/3 (100%)
    fact_evolution:   1/1 (100%), 1 violation — "Stripe" still in context (no consolidation)
    multi_hop:        0/1  (0%)  — Amsterdam pushed out of top-10 by noise, fails as expected
    noise_resistance: 0 violations
    opinion_arc:      1/1 (100%), 1 violation — "game changer" still in context (no opinion_view)
    OVERALL:          5/6 (83%), 2 violations

---

## v0.2.0 — HTTP contract surface and storage layer

Implements the six endpoints: POST /turns, POST /recall,
POST /search, GET /users/{user_id}/memories, DELETE /sessions/{session_id},
DELETE /users/{user_id}.

Implemented component tests in a separate `tests/`.

Implemented basic fixture scripts (`TestFixtureQuality`) is in place with five empty
YAML scenario files; it prints "no fixture data yet" and exits cleanly
until retrieval is wired.

---

## v0.1.1 — Switch reranker: TEI → Cohere Rerank API

The original self-hosted TEI container (BAAI/bge-reranker-v2-m3) is x86-only
and English-only — it fails on ARM64 and conflicts with the multilingual
nature of the memory content. Switches to the Cohere Rerank API
(rerank-multilingual-v3.0): no sidecar, no cold-start, 100+ languages.
Self-hosted alternatives (TEI ARM64, Infinity) either have unstable ML stacks
or add several minutes of cold-start per machine.

The trade-off is a new runtime dependency on an external API. Mitigated by
graceful degradation: when `COHERE_API_KEY` is unset the service falls back
to RRF-only ranking, keeping hybrid retrieval fully functional. The /health
endpoint no longer checks reranker liveness — it returns 200 when the
database is reachable, 503 otherwise.

---

## v0.1.0 — Initial architecture and skeleton

**Service layout.** Establishes a Go HTTP server using chi over PostgreSQL 16
with the pgvector extension. Dependencies: pgx v5 with pgxpool for connection
pooling, pgvector-go for type registration, go-openai for LLM calls in later
stages. Configuration is strictly via environment variables; no global state,
no external config libraries.

**Migrations.** SQL migration files are embedded in the binary via //go:embed
and applied on startup by an idempotent applier that tracks applied versions
in a schema_migrations table. No external migration tool.

**Schema design.** The schema is bi-temporal at the memory level. turns is an
append-only event log. memories carries supersedes pointers and active/valid_to
markers: contradicting facts mark the old row inactive rather than overwriting
it, preserving full history. entities and entity_relationships form a derived
navigation index for graph traversal; consistency is enforced via JOIN to
memories.active rather than cascade deletes.

**Storage choice.** A single Postgres instance holds turns, structured
memories, entities, relationships, and vector embeddings via pgvector. A
separate vector store (Qdrant, Weaviate) was rejected because it requires
dual-write coordination, which conflicts with the requirement that POST /turns
synchronously makes all extracted data queryable from /recall.

**Reranker (original plan).** The initial design called for a self-hosted TEI
container running BAAI/bge-reranker-v2-m3 as a sidecar. This choice is
revisited in v0.1.1.
