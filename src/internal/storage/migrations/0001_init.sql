-- ============================================================
-- Extensions
-- ============================================================
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================
-- TYPES
-- ============================================================
CREATE TYPE memory_type AS ENUM ('fact', 'preference', 'opinion', 'opinion_view', 'event');

-- ============================================================
-- TABLE: turns
-- Append-only event log of incoming conversation turns.
-- ============================================================
CREATE TABLE turns (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id      TEXT NOT NULL,
    user_id         TEXT,
    messages        JSONB NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL,
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    content_text    TEXT,
    content_tsv     TSVECTOR
);

CREATE INDEX idx_turns_session     ON turns(session_id);
CREATE INDEX idx_turns_user        ON turns(user_id) WHERE user_id IS NOT NULL;
CREATE INDEX idx_turns_user_ses    ON turns(user_id, session_id) WHERE user_id IS NOT NULL;
CREATE INDEX idx_turns_timestamp   ON turns(timestamp DESC);
CREATE INDEX idx_turns_content_fts ON turns USING GIN (content_tsv);

CREATE OR REPLACE FUNCTION turns_update_content() RETURNS TRIGGER AS $$
BEGIN
    NEW.content_text := (
        SELECT string_agg(msg->>'content', ' ' ORDER BY ord)
        FROM jsonb_array_elements(NEW.messages) WITH ORDINALITY AS arr(msg, ord)
    );
    NEW.content_tsv := to_tsvector('simple', COALESCE(NEW.content_text, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER turns_content_trigger
    BEFORE INSERT OR UPDATE OF messages ON turns
    FOR EACH ROW EXECUTE FUNCTION turns_update_content();

-- ============================================================
-- TABLE: memories
-- Structured knowledge with bi-temporal supersedes chains.
-- ============================================================
CREATE TABLE memories (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,
    type            memory_type NOT NULL,
    key             TEXT,
    value           TEXT NOT NULL,
    evidence        TEXT DEFAULT 'explicit',
    confidence      REAL DEFAULT 0.8 CHECK (confidence BETWEEN 0 AND 1),
    entities        JSONB DEFAULT '[]',
    valid_from      TIMESTAMPTZ,
    valid_to        TIMESTAMPTZ,
    supersedes      UUID REFERENCES memories(id) ON DELETE SET NULL,
    active          BOOLEAN NOT NULL DEFAULT true,
    embedding       VECTOR(1536),
    value_tsv       TSVECTOR GENERATED ALWAYS AS (
        to_tsvector('english', COALESCE(value, '') || ' ' || COALESCE(key, ''))
    ) STORED,
    source_session  TEXT,
    source_turn     UUID REFERENCES turns(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata        JSONB DEFAULT '{}'
);

CREATE INDEX idx_memories_user_active      ON memories(user_id, active);
CREATE INDEX idx_memories_user_type_active ON memories(user_id, type, active);
CREATE INDEX idx_memories_user_key_active  ON memories(user_id, key, active)
    WHERE active = true AND key IS NOT NULL;
CREATE INDEX idx_memories_supersedes       ON memories(supersedes) WHERE supersedes IS NOT NULL;
CREATE INDEX idx_memories_source_session   ON memories(source_session) WHERE source_session IS NOT NULL;
CREATE INDEX idx_memories_source_turn      ON memories(source_turn) WHERE source_turn IS NOT NULL;
CREATE INDEX idx_memories_created_at       ON memories(created_at DESC);
CREATE INDEX idx_memories_entities_gin     ON memories USING GIN (entities);
CREATE INDEX idx_memories_value_fts        ON memories USING GIN (value_tsv);

CREATE INDEX idx_memories_embedding_hnsw
    ON memories USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64)
    WHERE active = true;

-- ============================================================
-- TABLE: entities
-- ============================================================
CREATE TABLE entities (
    name            TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    canonical_name  TEXT NOT NULL,
    entity_type     TEXT,
    first_mentioned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    mention_count   INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (name, user_id)
);

CREATE INDEX idx_entities_user_canonical ON entities(user_id, canonical_name);
CREATE INDEX idx_entities_type           ON entities(user_id, entity_type)
    WHERE entity_type IS NOT NULL;

-- ============================================================
-- TABLE: entity_mentions
-- ============================================================
CREATE TABLE entity_mentions (
    memory_id       UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    entity_name     TEXT NOT NULL,
    user_id         TEXT NOT NULL,
    role            TEXT,
    PRIMARY KEY (memory_id, entity_name),
    FOREIGN KEY (entity_name, user_id) REFERENCES entities(name, user_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_mentions_entity_user ON entity_mentions(entity_name, user_id);
CREATE INDEX idx_mentions_memory      ON entity_mentions(memory_id);

-- ============================================================
-- TABLE: entity_relationships
-- Append-only graph edges. Consistency via JOIN to memories.active.
-- ============================================================
CREATE TABLE entity_relationships (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         TEXT NOT NULL,
    subject_entity  TEXT NOT NULL,
    predicate       TEXT NOT NULL,
    object_entity   TEXT NOT NULL,
    source_memory_id UUID NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_rel_subject_pred ON entity_relationships(user_id, subject_entity, predicate, created_at DESC);
CREATE INDEX idx_rel_object_pred  ON entity_relationships(user_id, object_entity, predicate, created_at DESC);
CREATE INDEX idx_rel_source       ON entity_relationships(source_memory_id);
