# Changelog

Architectural decisions and significant changes during development.
Entries are in reverse chronological order.

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
  - basic_facts:       3/3 hits (100%)
  - fact_evolution:    1/1 hits (100%) | violation: "Stripe" still in context (no consolidation)
  - multi_hop:         0/1 hits   (0%) | Amsterdam pushed out of top-10 by noise — fails as expected
  - noise_resistance:  0 violations    (negative test — checks not_expected_facts only)
  - opinion_arc:       1/1 hits (100%) | violation: "game changer" still in context (no opinion_view)
  - OVERALL:           5/6 (83%), 2 not-expected violations

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
