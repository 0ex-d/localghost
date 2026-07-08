// Package searchsql holds the search layer's DDL as constants. It is a LEAF package (imports nothing
// internal) so both the owner-run migration in hw/datastore and ghost.searchd's rebuild command can
// use it without a dependency cycle. Schema application runs as the ghost OWNER over the trust socket
// at unlock , the migrations-run-as-owner rule , so ghost_ro/ghost_rw inherit access via the
// per-schema grants below.
package searchsql

// SchemaCore is the FTS-capable core: originals, tombstones, chunks (no vector column), jobs,
// cue_history, citations, meta. It must apply on a stock Postgres 17 with contrib available.
//
// Two deliberate deviations from SPEC v1.1's literal DDL, both required for it to actually apply:
//   - unaccent() is STABLE, not IMMUTABLE, so it cannot appear in a GENERATED STORED column. The spec's
//     fts definition fails as written. search.immutable_unaccent wraps the two-argument dictionary form
//     and is declared IMMUTABLE (safe: the 'unaccent' dictionary is shipped config, not user-mutable on
//     this box).
//   - search.citations is an ADDITION: the spec's deletion phase 1 must find "every T1/T2 row whose
//     source list contains the deleted id", but the entry/memory tables live outside this spec and do
//     not exist yet. T1/T2 producers write (tier, ref_id, orig_source, orig_id) here at write time, so
//     invariant I4 is honorable from day one instead of waiting for the consolidation daemon.
const SchemaCore = `
CREATE SCHEMA IF NOT EXISTS search;
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE OR REPLACE FUNCTION search.immutable_unaccent(t text) RETURNS text AS
$$ SELECT public.unaccent('public.unaccent'::regdictionary, t) $$
LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT;

CREATE TABLE IF NOT EXISTS search.originals (
    id           bigint GENERATED ALWAYS AS IDENTITY,
    source       text        NOT NULL,
    sha256       bytea       NOT NULL,
    path         text        NOT NULL,
    captured_at  timestamptz NOT NULL,
    ingested_at  timestamptz NOT NULL DEFAULT now(),
    daemon       text        NOT NULL,
    meta         jsonb       NOT NULL DEFAULT '{}',
    PRIMARY KEY (source, id),
    UNIQUE (source, sha256)
) PARTITION BY LIST (source);

CREATE TABLE IF NOT EXISTS search.originals_email    PARTITION OF search.originals FOR VALUES IN ('email');
CREATE TABLE IF NOT EXISTS search.originals_message  PARTITION OF search.originals FOR VALUES IN ('message');
CREATE TABLE IF NOT EXISTS search.originals_document PARTITION OF search.originals FOR VALUES IN ('document');
CREATE TABLE IF NOT EXISTS search.originals_image    PARTITION OF search.originals FOR VALUES IN ('image');
CREATE TABLE IF NOT EXISTS search.originals_audio    PARTITION OF search.originals FOR VALUES IN ('audio');

CREATE INDEX IF NOT EXISTS originals_captured_at ON search.originals (captured_at);
CREATE INDEX IF NOT EXISTS originals_meta ON search.originals USING GIN (meta jsonb_path_ops);

ALTER TABLE search.originals_image ADD COLUMN IF NOT EXISTS phash bigint;
CREATE INDEX IF NOT EXISTS originals_image_phash ON search.originals_image (phash);

CREATE TABLE IF NOT EXISTS search.tombstones (
    sha256     bytea PRIMARY KEY,
    deleted_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS search.chunks (
    id          bigint GENERATED ALWAYS AS IDENTITY,
    tier        smallint    NOT NULL CHECK (tier IN (0,1,2)),
    orig_source text,
    orig_id     bigint,
    entry_id    bigint,
    memory_id   bigint,
    seq         int         NOT NULL DEFAULT 0,
    body        text        NOT NULL,
    fts         tsvector GENERATED ALWAYS AS
                  (to_tsvector('simple', search.immutable_unaccent(body))) STORED,
    stale       boolean     NOT NULL DEFAULT false,
    captured_at timestamptz NOT NULL,
    PRIMARY KEY (tier, id)
) PARTITION BY LIST (tier);

CREATE TABLE IF NOT EXISTS search.chunks_t0 PARTITION OF search.chunks FOR VALUES IN (0);
CREATE TABLE IF NOT EXISTS search.chunks_t1 PARTITION OF search.chunks FOR VALUES IN (1);
CREATE TABLE IF NOT EXISTS search.chunks_t2 PARTITION OF search.chunks FOR VALUES IN (2);

DO $$ BEGIN
  ALTER TABLE search.chunks_t0
    ADD CONSTRAINT chunks_t0_orig_fk FOREIGN KEY (orig_source, orig_id)
    REFERENCES search.originals (source, id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE INDEX IF NOT EXISTS chunks_fts ON search.chunks USING GIN (fts);
CREATE INDEX IF NOT EXISTS chunks_captured_at ON search.chunks (captured_at);

CREATE TABLE IF NOT EXISTS search.citations (
    tier        smallint NOT NULL CHECK (tier IN (1,2)),
    ref_id      bigint   NOT NULL,
    orig_source text     NOT NULL,
    orig_id     bigint   NOT NULL,
    PRIMARY KEY (tier, ref_id, orig_source, orig_id)
);
CREATE INDEX IF NOT EXISTS citations_by_orig ON search.citations (orig_source, orig_id);

CREATE TABLE IF NOT EXISTS search.jobs (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text        NOT NULL,
    payload    jsonb       NOT NULL,
    run_after  timestamptz NOT NULL DEFAULT now(),
    attempts   int         NOT NULL DEFAULT 0,
    last_error text
);
CREATE INDEX IF NOT EXISTS jobs_kind_run ON search.jobs (kind, run_after);

CREATE TABLE IF NOT EXISTS search.cue_history (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    memory_id    bigint      NOT NULL,
    context_type text        NOT NULL,
    surfaced_at  timestamptz NOT NULL DEFAULT now(),
    engaged      boolean     NOT NULL DEFAULT false
);
CREATE INDEX IF NOT EXISTS cue_hist_idx ON search.cue_history (memory_id, context_type, surfaced_at);

CREATE TABLE IF NOT EXISTS search.meta (
    key   text PRIMARY KEY,
    value text NOT NULL
);
`

// SchemaVector adds the pgvector pieces: the emb columns and per-partition HNSW indexes. Applied only
// if CREATE EXTENSION vector succeeds; its absence is the documented FTS-only degraded mode, recorded
// in search.meta('vector') so searchd's health can say so instead of guessing.
const SchemaVector = `
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE search.chunks ADD COLUMN IF NOT EXISTS emb vector(768);
ALTER TABLE search.chunks ADD COLUMN IF NOT EXISTS emb_model text;
CREATE INDEX IF NOT EXISTS chunks_t0_hnsw ON search.chunks_t0 USING hnsw (emb vector_cosine_ops) WITH (m=16, ef_construction=64);
CREATE INDEX IF NOT EXISTS chunks_t1_hnsw ON search.chunks_t1 USING hnsw (emb vector_cosine_ops) WITH (m=16, ef_construction=64);
CREATE INDEX IF NOT EXISTS chunks_t2_hnsw ON search.chunks_t2 USING hnsw (emb vector_cosine_ops) WITH (m=16, ef_construction=64);
INSERT INTO search.meta (key, value) VALUES ('vector', 'on') ON CONFLICT (key) DO UPDATE SET value = 'on';
`

// SchemaNoVector records the degraded mode when pgvector is unavailable.
const SchemaNoVector = `
INSERT INTO search.meta (key, value) VALUES ('vector', 'off') ON CONFLICT (key) DO UPDATE SET value = 'off';
`

// HealthView is the single ops view (spec 13.4), tolerant of the no-vector mode (pending_embeds is
// meaningless without an emb column, so the view is created in the vector branch only; the fallback
// creates the reduced view).
const HealthView = `
CREATE OR REPLACE VIEW search.health AS
SELECT
  (SELECT count(*) FROM search.chunks WHERE emb IS NULL AND NOT stale) AS pending_embeds,
  (SELECT count(*) FROM search.chunks WHERE stale)                     AS stale_chunks,
  (SELECT count(*) FROM search.jobs  WHERE attempts >= 5)              AS parked_jobs,
  (SELECT count(*) FROM search.jobs  WHERE run_after <= now() AND attempts < 5) AS runnable_jobs;
`

const HealthViewNoVector = `
CREATE OR REPLACE VIEW search.health AS
SELECT
  0::bigint                                                            AS pending_embeds,
  (SELECT count(*) FROM search.chunks WHERE stale)                     AS stale_chunks,
  (SELECT count(*) FROM search.jobs  WHERE attempts >= 5)              AS parked_jobs,
  (SELECT count(*) FROM search.jobs  WHERE run_after <= now() AND attempts < 5) AS runnable_jobs;
`

// Grants gives the service roles access to the search schema. The earlier role grants covered schema
// public only; without these, searchd (ghost_rw) cannot see its own tables , the exact
// default-privileges tripwire, handled here at the source.
const Grants = `
GRANT USAGE ON SCHEMA search TO ghost_ro, ghost_rw;
GRANT SELECT ON ALL TABLES IN SCHEMA search TO ghost_ro;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA search TO ghost_rw;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA search TO ghost_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA search GRANT SELECT ON TABLES TO ghost_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA search GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ghost_rw;
ALTER DEFAULT PRIVILEGES IN SCHEMA search GRANT USAGE, SELECT ON SEQUENCES TO ghost_rw;
`
