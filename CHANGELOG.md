# Changelog

Architectural decisions and significant changes during development.
Entries are in reverse chronological order.

---

## v0.1.1 — Switch reranker: TEI → Cohere Rerank API

**Problem with TEI.** The original reranker — a self-hosted TEI container
running BAAI/bge-reranker-v2-m3 — is x86-only. On ARM64 (Apple Silicon,
AWS Graviton) it either refuses to start or fails to download model weights
under Rosetta 2 emulation due to a network resolution bug in the emulated
environment. More fundamentally, it is English-only, which conflicts with
the multi-language nature of the memory content this service stores.

**Decision.** Switches to the Cohere Rerank API (rerank-multilingual-v3.0).
The model supports 100+ languages including Russian, Arabic, and CJK scripts.
No sidecar container, no model-cache volume, no cold-start delay — the service
makes an outbound HTTPS call per /recall request.

**Alternatives considered.** TEI ARM64 builds exist but the ML stack is
unstable under native ARM64 at the time of writing. Infinity (a multi-arch
self-hosted serving platform) works but adds container weight and 3-5 minutes
of cold-start per machine — unacceptable for an eval environment we don't
control.

**Trade-off accepted.** The service now depends on an external API at recall
time. This is mitigated by graceful degradation: when COHERE_API_KEY is unset,
retrieval falls back to RRF-only ranking. Hybrid retrieval (semantic + keyword
+ graph + temporal) remains fully in effect; only the final cross-encoder pass
is skipped.

**Simplification.** The /health endpoint no longer checks reranker liveness —
it returns 200 ok when the database is reachable, 503 when not. The previous
"degraded" state (tied to TEI container liveness) is removed.

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
