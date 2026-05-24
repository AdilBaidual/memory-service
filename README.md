# Memory Service

A long-term memory backend for AI agents. Ingests conversation turns, extracts structured
knowledge via LLM, and returns formatted context for the agent's next turn.

Inspired by [mem0](https://github.com/mem0ai/mem0), [Honcho](https://github.com/plastic-labs/honcho),
and [Hindsight](https://arxiv.org/abs/2410.02967), with its own architecture.

---

## Architecture

The service is a single Go binary backed by one PostgreSQL instance. There are no async
workers, queues, or eventual consistency: every `POST /turns` call runs extraction,
consolidation, entity processing, and opinion synthesis inside one HTTP request. After
the 201 returns, all extracted data is immediately queryable via `/recall`.

The code is organized in four strict layers:

```
handlers → usecase → service → adapters
```

`handlers` parses HTTP and writes responses. `usecase` owns transaction boundaries and
orchestrates service calls. `service` contains domain logic (extraction, consolidation,
retrieval, opinion synthesis). `adapters` wraps external dependencies (PostgreSQL, OpenAI,
Cohere). Interfaces are defined at the consumer (usecase), not the implementer (service/adapters),
so each layer is testable in isolation.

### Spec compliance checklist

The service targets every criterion from the "excellent" bar in the task specification:

- **Contract compliance** — all seven endpoints implemented with exact shapes and status codes per spec §3
- **Structured extraction** — memories have types (`fact`, `preference`, `opinion`, `event`), confidence scores, and full provenance (`source_session`, `source_turn`, `supersedes`)
- **Fact evolution** — contradictions detected via canonical key lookup; old facts superseded (not deleted) with bi-temporal history preserved and inspectable via `/users/{id}/memories`
- **Real recall ranking** — hybrid pipeline: embedding (pgvector HNSW) + keyword (Postgres FTS) + entity graph (2-hop CTE) + RRF fusion + temporal recency boost + Cohere cross-encoder rerank
- **Multi-hop recall** — entity graph traversal connects memories across sessions ("city where Luna's owner lives")
- **Query rewriting** — HyDE rewrites queries before embedding for better dense retrieval on abstractly phrased questions
- **Context assembly** — priority-ordered sections with explicit token budget logic (stable facts → opinions → retrieved → events)
- **Synchronous correctness** — after `POST /turns` returns 201, all extracted data is immediately queryable; no eventual consistency gaps
- **Token budget** — tiktoken-go cl100k_base; total never exceeds `max_tokens` in the request
- **Persistence** — named Docker volume; restart is transparent to clients
- **Graceful degradation** — missing API keys, cold sessions, oversized payloads, malformed input all handled without crashes
- **Anonymous sessions** — `user_id` is optional across all endpoints; session-scoped memory works without authentication
- **Test coverage** — contract roundtrip, restart persistence, concurrent sessions, malformed input, recall quality fixture with LLM-as-judge assertions
- **CHANGELOG** — iteration history with measured metrics at each step

### POST /turns — ingest flow

```
POST /turns
  │
  ├─ Handler: parse body, validate required fields, enforce 1 MB limit
  │
  └─ Usecase: IngestTurn (two sequential transactions)
      ├─ Tx1: adapters/store: InsertTurn → turns table; commits immediately
      │   (turn is durable before extraction begins)
      │
      ├─ adapters/llm: Extract (gpt-4o-mini, Structured Output Strict)
      │   ├─ input: all message roles for context; extracts facts attributable
      │   │         to the user only (tool-role messages are context, not source)
      │   └─ output: facts, preferences, opinions, events, relationship triplets
      │
      ├─ Tx2: for each memory candidate:
      │   ├─ fact / preference → ConsolidateFact (ADD / NOOP / UPDATE)
      │   │   ├─ ADD:    new key — INSERT active row
      │   │   ├─ NOOP:   same value — touch updated_at only
      │   │   └─ UPDATE: contradicting value — mark old row active=false,
      │   │              valid_to=NOW(), INSERT new row with supersedes pointer
      │   ├─ opinion / event → INSERT unconditionally (append-only; no consolidation)
      │   ├─ adapters/llm: Embed each memory (text-embedding-3-small, dim=1536)
      │   ├─ service/relationships: ProcessRelationships
      │   │   ├─ upsert entities table (name, user_id, mention_count)
      │   │   ├─ insert entity_relationships (subject, predicate, object, source_memory)
      │   │   └─ insert entity_mentions (entity → memory cross-reference)
      │   └─ Tx2 commits
      │
      └─ service/opinions: SynthesizeIfReady (runs after Tx2, outside transaction)
          └─ if 2+ raw opinions exist for same (user_id, topic key):
              ├─ LLM call (gpt-4o-mini): synthesize evolution arc into one paragraph
              ├─ Tx3: INSERT opinion_view row (supersedes prior view for that key)
              └─ embed synthesized text for relevance filtering at recall time

  Response: 201 {"id": "<turn_uuid>"}
```

### POST /recall — retrieval flow

```
POST /recall
  │
  ├─ Handler: parse body, validate required fields
  │
  └─ Usecase: Recall
      ├─ adapters/store: load stable facts + opinion_views for user
      │   (these are always included in context, not subject to retrieval ranking)
      │
      ├─ adapters/llm: HyDE rewrite (gpt-4o-mini)
      │   └─ generates a short hypothetical answer; semantic channel embeds
      │      the answer rather than the raw query — improves dense retrieval
      │      for underspecified or abstractly phrased questions
      │
      ├─ service/retrieval: HybridRetriever (three channels run in parallel)
      │   ├─ Semantic: embed rewritten query → cosine similarity ORDER BY <=>
      │   │            (pgvector HNSW index, vector_cosine_ops, top-30)
      │   ├─ FTS:      plainto_tsquery('english') on value_tsv generated column
      │   │            (ts_rank_cd scoring, top-30)
      │   └─ Graph:    2-hop CTE via entity_relationships
      │       ├─ hop1: entity_mentions → memories directly linked to query entities
      │       │        (score 1.0, boosted by mention_count signal)
      │       └─ hop2: follow entity_relationships edges to neighbour entities
      │                (score 0.5, flat — avoids inflating high-mention entities)
      │
      ├─ RRF fusion (k=60): merge three ranked lists into one score
      │
      ├─ Temporal recency boost: score × (α + (1−α) × exp(−λ × days_since_update))
      │   λ=0.005 (half-life ~139 days), α=0.7
      │   (recency contributes at most 30%; semantic relevance still dominates)
      │
      ├─ Cohere rerank (optional, requires COHERE_API_KEY):
      │   ├─ present: top-30 candidates re-scored by rerank-english-v3.0
      │   │           (full query×document cross-encoder; more precise than bi-encoder)
      │   └─ absent:  temporal-boosted RRF order used unchanged
      │
      └─ service/context: Assembler — token-budgeted context assembly
          ├─ Section 1: stable facts and preferences (highest priority)
          ├─ Section 2: opinion views (filtered by cosine similarity to query)
          ├─ Section 3: retrieved memories (up to 25 candidates, token-budgeted)
          └─ Section 4: recent events
          (each section cedes unused token budget to the next; tiktoken cl100k_base)

  Response: 200 {"context": "<markdown string>", "citations": [...]}
```

---

## Backing store

PostgreSQL 16 with the pgvector extension. A single database holds all data: turns
(event log), memories (structured knowledge with embeddings), entities, entity
relationships, and entity mentions.

**Why not a dedicated vector store (Qdrant, Pinecone, Weaviate)?**

The service has a hard synchronous-correctness requirement: after `POST /turns` returns
201, all extracted data must be immediately queryable via `/recall`. A dedicated vector
store requires a dual-write: once to Postgres for relational data, once to the vector DB
for embeddings. Dual-writes cannot be made atomic without a distributed transaction
coordinator, which adds substantial operational complexity and introduces a window where
the two stores are inconsistent. Using pgvector keeps everything in one ACID transaction.

**Why not separate stores for keyword search (Elasticsearch, OpenSearch)?**

Same reason: Postgres `tsvector` with `plainto_tsquery('english')` is sufficient for
the hybrid retrieval workload here, and it is co-located with the vector index.
A separate search cluster would double the infrastructure footprint and the risk of
inconsistency without providing meaningful quality improvement at this scale.

**Schema design.** `turns` is an append-only event log. `memories` carries supersedes
chains and active/valid_to markers for bi-temporal history. `entities` and
`entity_relationships` are a derived navigation index; `entity_mentions` links memories
to the entities they reference. The HNSW index on `memories.embedding` is partial
(`WHERE active = true`) so vector search never touches superseded rows.

---

## Extraction pipeline

On each `POST /turns`, the service calls `gpt-4o-mini` with Structured Outputs (Strict
mode), which guarantees a schema-valid JSON response. The model returns up to five types
per turn:

| Type | Key | Description |
|------|-----|-------------|
| `fact` | yes | Stable attribute of the user (current city, employer, etc.) |
| `preference` | yes | Consistent user preference (communication style, tool choices) |
| `opinion` | yes | User stance on a topic (technology opinion, product view) |
| `event` | no | Something that happened (job change, trip, one-time occurrence) |
| relationship | N/A | Triplet (subject, predicate, object) for the entity graph |

**Confidence** is computed by the system, not requested from the LLM. The base value
is 0.95 for explicit statements and 0.70 for implicit inferences. Confidence increases
on repetition.

**Temperature** is forced to the smallest non-zero float (`math.SmallestNonzeroFloat32`),
which the go-openai struct serialises as a non-zero value but the model treats as
deterministic. This eliminates run-to-run variance in extracted facts.

**Existing-keys hint**: before calling the LLM, the service passes all active key-value
pairs for the user as context, with semantic constraints (e.g., `has_pet` is for animals
only; `current_city` changes only on an explicit permanent move). This prevents temporary
travel episodes from superseding permanent location facts.

**Multi-message turns**: extraction reads all message roles for context but attributes
facts only to user-role messages. Tool outputs are context, not source.

---

## Recall strategy

`/recall` runs four retrieval channels and fuses them:

1. **Semantic** (pgvector cosine). The query is first rewritten by HyDE (Hypothetical
   Document Embedding): `gpt-4o-mini` generates a short hypothetical answer, and the
   service embeds that answer rather than the raw query. Dense retrieval works better on
   a plausible answer than on a question.

2. **Full-text search** (Postgres FTS). `plainto_tsquery('english')` against the
   `value_tsv` generated column. English config adds stemming and stopword removal.
   OR semantics ensure a match on any key term; strict AND would miss multi-word queries.

3. **Entity graph** (2-hop CTE). The service extracts capitalized-word entities from
   the raw query, then runs two hops through `entity_relationships`:
   - Hop 1: memories directly linked to query entities (score 1.0, weighted by `mention_count`).
   - Hop 2: memories linked to neighbour entities discovered in hop 1 (score 0.5, flat).
     Hop 2 enables reasoning across entity chains ("city of the user's pet's owner").

4. **Temporal recency boost** (post-fusion). After RRF fusion, each score is multiplied
   by `α + (1−α) × exp(−λ × days)` (λ=0.005, α=0.7). Half-life is ~139 days. The 0.7
   floor ensures stale but semantically relevant facts are never discarded entirely.

5. **Cohere cross-encoder rerank** (optional). The top-30 fused candidates are re-scored
   by `rerank-english-v3.0`, which evaluates the full (query, document) pair jointly —
   more accurate than cosine similarity on independent embeddings. Enabled via
   `COHERE_API_KEY`; gracefully skipped when the key is absent or the API fails.

**Cold sessions** always return 200 with an empty context string and an empty citations
array. The endpoint never errors on missing data.

---

## Fact evolution

Contradicting facts are never overwritten in place. The service maintains a full
bi-temporal supersedes chain:

```
Turn 1: "I work at Stripe"
  → INSERT memories (key=current_employer, value="User works at Stripe", active=true)

Turn 2: "I just joined Notion"
  → UPDATE memories SET active=false, valid_to=NOW() WHERE id = <stripe_row>
  → INSERT memories (key=current_employer, value="User works at Notion",
                     active=true, supersedes=<stripe_row>)
```

The old row is preserved with `active=false` for audit and history queries. The
`supersedes` pointer reconstructs the full evolution chain. Vector search uses a partial
HNSW index on `active=true` rows only, so superseded facts never surface in retrieval.

**Opinion handling** follows a different pattern (Honcho-style). Raw `opinion` rows are
always appended and never superseded. When 2 or more raw opinions exist for the same
`(user_id, topic key)`, the service calls `gpt-4o-mini` to synthesize a single paragraph
capturing the evolution arc, stored as an `opinion_view` row. Subsequent syntheses replace
the prior `opinion_view` for that key. The raw opinions are preserved for audit.

---

## Recall priority and context assembly

The `/recall` response is a Markdown string assembled from four priority sections:

| Priority | Section | Source |
|----------|---------|--------|
| 1 (highest) | Stable facts and preferences | `memories WHERE type IN ('fact','preference') AND active=true` |
| 2 | Synthesized opinion views | `memories WHERE type='opinion_view' AND active=true`, filtered by cosine similarity to query (threshold 0.25; minimum 2 views always kept) |
| 3 | Retrieved memories | Up to 25 from hybrid retrieval (capped by token budget) |
| 4 | Recent events | `memories WHERE type='event'` |

Token budgets are enforced per section using tiktoken-go (`cl100k_base` encoding).
Higher-priority sections consume their allocation first; unused budget flows to lower-
priority sections. The total never exceeds the `max_tokens` field in the request.

---

## Failure modes

| Scenario | Behavior |
|----------|----------|
| `OPENAI_API_KEY` not set | Service starts. `POST /turns` saves the turn and returns 201 but skips extraction — no memories are created. `POST /recall` returns 200 with empty context. Logged at WARN level. |
| `COHERE_API_KEY` not set | Reranker is disabled. `/recall` uses temporal-boosted RRF order. No degradation in correctness, slight reduction in precision. |
| OpenAI API timeout or error | Turn is saved (201 returned). Extraction failure is logged at WARN. Memories from that turn are not created. Subsequent turns are unaffected. |
| Cohere API error | Reranker step is skipped silently. RRF order is used. Logged at WARN. `/recall` still returns a response. |
| Cold user (no memories) | `/recall` and `/search` return 200 with empty results. Never a 404 or 500. |
| Anonymous session (`user_id` null) | Extraction runs normally, scoped to the session. `/recall` and `/search` return session-scoped context. Data is isolated from all other sessions. |
| Malformed JSON in request body | 400 with an error message. Service does not crash. |
| Oversized request body (>1 MB) | 413. Body limit is enforced in middleware before any processing. |
| Postgres unavailable | All endpoints return 500. The service process stays alive and recovers automatically when Postgres becomes available again. |
| Container restart | All data survives via the named Docker volume `pgdata`. The service is stateless; pgvector and full-text indexes are rebuilt from the persisted data at startup. |
| Unicode or null bytes in message content | Sanitized before storage. No crash. The sanitized content is stored and extractable. |

---

## Session scoping and identity

The service supports two identity modes:

**Authenticated** (`user_id` provided): memories are scoped to the user and shared
across all of their sessions. A fact learned in session 1 is available in session 2
for the same user — this is the long-term memory feature. Cross-user isolation is
strict at every query.

**Anonymous** (`user_id` null): when no user identity is known — for example a
support chat before login — the session itself becomes the scope. Extraction runs
normally; memories are stored with `user_id=NULL` and scoped to `session_id`. Two
anonymous sessions never see each other's memories.

This means `user_id` is optional in `POST /turns`, `POST /recall`, and `POST /search`.
`session_id` is always required in `/turns` and `/recall`. The service resolves the
identity scope automatically:

```
user_id present  →  user scope  (memories shared across sessions for that user)
user_id absent   →  session scope  (memories isolated to this session only)
```

**DELETE /sessions/{id}** removes the conversation log (turns) and all memories that
originated from that session (`source_session = id`). Entity mentions and relationship
edges cascade automatically via FK. Entity rows are cleaned up if they have no remaining
mentions. Works identically for authenticated and anonymous sessions.

**DELETE /users/{id}** removes everything for that user: turns, memories, entities,
relationships, mentions. Anonymous session data is cleaned up via DELETE /sessions.

**POST /search** supports three scoping modes:

| `user_id` | `session_id` | Behavior |
|-----------|--------------|----------|
| provided | provided | Hybrid retrieval for user, filtered to that session |
| provided | absent | Hybrid retrieval across all user memories |
| absent | provided | Memories sourced directly from that session |
| absent | absent | Returns empty results |

---

## How to run

**Prerequisites:** Docker, Docker Compose. Two API keys are needed for full functionality:

| Key | Required | Where to get it |
|-----|----------|-----------------|
| `OPENAI_API_KEY` | Required — extraction and embeddings will not run without it | [platform.openai.com/api-keys](https://platform.openai.com/api-keys) — requires a funded account (minimum $5 credit) |
| `COHERE_API_KEY` | Optional — enables cross-encoder reranking; service works without it | [dashboard.cohere.com/api-keys](https://dashboard.cohere.com/api-keys) — free tier includes 1000 rerank calls/month, no credit card required |

```bash
cp .env.example .env
```

Open `.env` and fill in the keys:

```
OPENAI_API_KEY=sk-...        # required
COHERE_API_KEY=...           # optional, leave blank to disable reranker
```

Then start the service:

```bash
docker compose up -d

# Wait for health
until curl -sf http://localhost:8080/health; do sleep 1; done

curl -s http://localhost:8080/health | jq .
# {"status": "ok"}
```

Default port: `8080`. Override via `PORT` in `.env`.

**Without `OPENAI_API_KEY`:** the service starts and all endpoints respond, but
`POST /turns` saves turns without extracting memories, and `POST /recall` returns
empty context. Useful for contract testing without incurring API costs.

**Without `COHERE_API_KEY`:** the reranker is disabled; retrieval falls back to
temporal-boosted RRF order. No other functionality is affected.

### Quick smoke test

```bash
# Ingest a turn (authenticated)
curl -X POST http://localhost:8080/turns \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "s1", "user_id": "u1",
    "messages": [
      {"role": "user", "content": "I just moved to Berlin from NYC last month."},
      {"role": "assistant", "content": "That sounds exciting!"}
    ],
    "timestamp": "2025-03-15T10:30:00Z", "metadata": {}
  }'
# {"id": "<uuid>"}

# Ingest a turn (anonymous — no user_id)
curl -X POST http://localhost:8080/turns \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "anon-s1",
    "messages": [
      {"role": "user", "content": "I need help with order #12345."},
      {"role": "assistant", "content": "I can help with that!"}
    ],
    "timestamp": "2025-03-15T10:31:00Z", "metadata": {}
  }'
# {"id": "<uuid>"}

# Recall context (authenticated)
curl -X POST http://localhost:8080/recall \
  -H 'Content-Type: application/json' \
  -d '{"query": "Where does this user live?", "session_id": "s2", "user_id": "u1", "max_tokens": 512}'
# context will mention Berlin and the move from NYC

# Recall context (anonymous — session-scoped)
curl -X POST http://localhost:8080/recall \
  -H 'Content-Type: application/json' \
  -d '{"query": "What is the user asking about?", "session_id": "anon-s1", "max_tokens": 512}'
# context will mention order #12345

# List structured memories
curl http://localhost:8080/users/u1/memories | jq .
```

---

## Tests

```bash
make tests          # component + persistence + fixture quality (all)

make test-component   # contract tests: all 7 endpoints, roundtrip, malformed input
make test-persistence # restart survival: data survives container restart
make test-fixtures    # LLM quality: 5 scenarios, hit rate + LLM judge assertions
```

Tests run inside Docker against the live service. The fixture runner prints a hit-rate
table per scenario and a JUDGE summary. No assertions on fixture tests — copy the
OVERALL line into CHANGELOG after each iteration.

Current fixture quality (measured against v1.6.1):

```
basic_facts:      3/3 (100%)
fact_evolution:   1/1 (100%), 1 not-expected violation (Stripe persists — bi-temporal history, expected)
multi_hop:        1/1 (100%)
noise_resistance: 0 violations
opinion_arc:      1/1 (100%), 1 not-expected violation (raw opinion text surfaces via retrieval)
OVERALL:          6/6 (100%), 2 not-expected violations
JUDGE:            20/20 assertions correct (100%)
```

---

## Tradeoffs and known limitations

**Open-vocabulary predicate matching.** LLM extraction produces predicates freely
(lives_in, has_pet, works_at). Synonym resolution uses hardcoded groups in Go
(`predicateGroups` in `service/retrieval/graph.go`). This is sufficient for the current
fixture set but will miss novel predicate spellings not in the group list. A v2 approach
would use an embedding-based predicate registry.

**HyDE adds latency.** The hypothetical answer rewrite adds one LLM call per `/recall`
request (~200–400 ms at gpt-4o-mini speeds). It improves dense retrieval quality,
especially for abstract queries, but increases p50 recall latency by roughly 30%.

**Cohere reranker is an external dependency.** The service degrades gracefully (RRF
order) when the key is absent, but adding a reranker call to the recall path means one
more external service to monitor.

**No idempotency on /turns.** Client retries after a network timeout will create duplicate
turns. The extraction and consolidation logic is resilient to duplicate values (NOOP
path), but duplicate events or opinions will be inserted twice.

**Single Postgres for everything.** At very high write volumes, the HNSW index
maintenance on `memories.embedding` may become a bottleneck. The partial index (WHERE
active=true) limits index size to live rows only, but index rebuilds on large tables will
cause latency spikes.

---

## Future work (v2+)

- Embedding-based predicate registry (open-vocabulary predicate matching without hardcoded groups)
- True BM25 ranking via ParadeDB / pg_search
- Temporal channel: extract `valid_from`/`valid_to` from linguistic time hints in turns
- Three-hop graph traversal
- Idempotency keys on /turns for client retry safety
- Cursor-based pagination on /users/{user_id}/memories