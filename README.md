# Memory Service

A memory service for AI agents. Ingests conversation turns, extracts structured
knowledge, and returns formatted context for the agent's next turn.

<!-- TODO: brief overview after Stage 6 -->

## Architecture

<!-- TODO: diagram and prose after Stage 4-5 -->

## Backing store

PostgreSQL 16 with the pgvector extension. Single database holds turns,
structured memories, entity graph, and embeddings.

<!-- ARCHITECTURAL NOTE — do not delete, expand in later stages:
Chosen for transactional consistency (synchronous-correctness requirement),
unified operational footprint, and integrated hybrid search (vector +
tsvector + JSONB) in single SQL queries. Alternative (Qdrant/Weaviate)
rejected because dual-write coordination would compromise the requirement
that /turns synchronously make all data queryable. -->

## Extraction pipeline

<!-- TODO after Stage 3 -->

<!-- ARCHITECTURAL NOTE:
LLM-based extraction using OpenAI gpt-4o-mini with Structured Outputs
(Strict mode) for guaranteed schema-valid JSON. Each turn produces facts,
preferences, opinions, events, and entity relationships (triplets).
Confidence is computed by the system (not requested from LLM) based on
evidence type (explicit/implicit) and repetition signals. -->

## Recall strategy

<!-- TODO after Stage 4-5 -->

<!-- ARCHITECTURAL NOTE:
Hybrid retrieval inspired by Hindsight's TEMPR pipeline: semantic
(pgvector cosine) + keyword (Postgres FTS, simple config) + graph
(entity relationships via CTE, up to 2-hop) + temporal (recency
multiplier). Channels fused via Reciprocal Rank Fusion (k=60). Top-30
reranked by cross-encoder via TEI (BAAI/bge-reranker-v2-m3 in a sidecar
container). Final top-10 passed to Context Assembler.

Cold sessions return 200 with empty context, never error.

Multi-message turns including tool-role messages: extraction reads all
roles for context but only extracts facts attributable to the user, not
to tool outputs. -->

## Fact evolution

<!-- TODO after Stage 3 -->

<!-- ARCHITECTURAL NOTE:
Bi-temporal model. `turns` table is append-only (event log). `memories`
table has explicit supersedes chains: on contradicting fact, old row
marked active=false with valid_to=NOW(), new row inserted with
active=true and supersedes pointing to old.

`entity_relationships` is the derived navigation index for graph
traversal; it is append-only and reads filter through JOIN to
memories.active for consistency. -->

## Recall priority and context assembly

<!-- TODO after Stage 5 -->

<!-- ARCHITECTURAL NOTE:
Context is composed from independent sections in priority order:
1. Stable facts (core profile: current_city, current_employer, etc.)
2. Opinion views (synthesized representations per topic, Honcho-style)
3. Query-relevant memories (output of retrieval pipeline)
4. Recent context (last N turns of current session)

Each section has soft token budget. Unused budget flows to next section.
Final output respects max_tokens (won't exceed 2x per ToA). Token counts
computed with tiktoken-go using cl100k_base encoding. -->

## Concurrent sessions and scoping

Memories are scoped to user_id and shared across all sessions of that user
(this IS the long-term memory feature). Recent conversation context in
/recall is scoped to session_id. Cross-user isolation is strict.

<!-- ARCHITECTURAL NOTE:
DELETE /sessions/{id} removes all data that originated from the session:
turns and all memories tagged with source_session (entity_mentions and
entity_relationships cascade automatically via FK). Entities themselves
survive — they are user-scoped and may have been referenced by other
sessions. DELETE /users/{id} removes everything across all sessions. -->

## Tradeoffs

<!-- TODO after Stage 6 -->

## Failure modes

- Empty/cold session: `/recall` returns 200 with empty context, never errors
- Malformed request body: 400 with descriptive error
- Body over 1MB: 413
- LLM unavailable/timeout: extraction skipped, turn saved without memories,
  service stays up
- Reranker (Cohere API) unreachable or COHERE_API_KEY not set: skip
  reranking step, fall back to RRF ordering. Service stays up, /recall
  continues to work with slightly reduced quality.
- API keys missing at startup: service starts in degraded mode

<!-- TODO: expand each after Stages 2-5 -->

## How to run

```bash
cp .env.example .env
# (edit .env to add OPENAI_API_KEY if you want extraction to work)
docker compose up -d

# wait for health
until curl -sf http://localhost:8080/health; do sleep 1; done

# verify
curl -s http://localhost:8080/health | jq .
```

Default port: 8080. Override via `PORT` in `.env`.

## Tests

<!-- TODO after Stage 6 -->

## Future iterations

See [CHANGELOG.md](./CHANGELOG.md) for completed iterations.

Planned for v2+:
- Multi-stage fact matching (semantic similarity + LLM judge for ambiguous key variations)
- True BM25 ranking via ParadeDB / pg_search extension
- Query rewriting via LLM at recall time
- Temporal channel: full bi-temporal extraction with valid_from/valid_to from time hints
- Reflect operation: periodic background synthesis of opinion arcs into mental_model memories
- Three-hop graph traversal
- Dynamic predicate matching via embeddings (open-vocabulary predicate registry)
- Idempotency keys on /turns for client retry safety
- Cursor-based pagination on /users/{user_id}/memories
