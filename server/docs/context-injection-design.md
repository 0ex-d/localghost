# Context injection , from passthrough to grounded answers

Status: retrieval seam is LIVE in ghost.synthd's chat handler as of this commit, wired to
ghost.searchd's `search` command. The index is empty, so today every chat is a byte-identical
passthrough to ghost.oracled. This document is the plan for making it non-empty, in honest phases.

## What already exists (build on it, do not reinvent)

- `internal/search`: full retrieval stack. FTS + vector legs, RRF fusion, ranking, chunker,
  `VisionOracle.Caption` (image -> text via oracled's multimodal path), phash dedup, ingest worker.
- `internal/searchsql`: the search schema DDL (originals, chunks, citations, jobs), applied as owner
  at unlock. FTS works on stock Postgres 17; vectors are optional (vector-less boxes stay silent).
- `ghost.searchd` ctlsock commands: `search` (query, filters, limit -> dated snippets), `ingest`,
  `ingest-t1/t2`, `delete`, `rebuild`, `prime`.
- `ghost.synthd` chat handler: retrieves via `search`, prepends a dated context block, falls through
  to bare passthrough on empty/error/timeout (3s budget , retrieval never stalls a question).
- Staged but not serving: embeddinggemma-300m-q8.gguf on the volume for the embedding leg.

## Phase 1 , captions make photos searchable (the unlock for everything)

framed archives a photo, then hands it to search ingest with a caption:

1. framed, after archiving, enqueues the frame (hash, path, taken_at, gps) to searchd `ingest`
   with source `image` and empty text.
2. A caption worker (searchd's existing job queue fits) pulls uncaptioned image originals, calls
   `VisionOracle.Caption` (oracled multimodal, background priority so chat always preempts), stores
   the caption as the chunk text. Rate: one at a time, only while the box is otherwise idle , the
   12B on CPU is a shared scarce resource and photos are patient.
3. Tier: captions land as tier 0 (raw derived). No summarisation yet.

Cost estimate, honest: ~2900 photos x ~10s per caption on CPU = about 8 hours of idle time. It runs
unattended and resumes across unlocks (job queue is in the schema). GPS coordinates can be attached
to the caption text later via reverse geocoding, offline (a coarse city-level lookup table on the
volume), never via a network call.

## Phase 2 , the embedding leg

Second llama-server instance, embedding mode, embeddinggemma-300m on a second loopback port, managed
by oracled beside the chat engine (300M is ~300MB resident, cheap). searchd's embedder then fills
vectors for existing chunks (backfill job) and new ones inline. Retrieval quality jumps from lexical
to semantic , "photos from that trip with the volcano" starts working without the word volcano in
any caption.

## Phase 3 , conversation memory

Chat history is not stored anywhere today (each question stands alone). Store turns in the search
schema as source `chat`, tier 1, and the same retrieval automatically gives the model memory of past
conversations. Requires a retention decision from the operator first: chats are the most sensitive
text on the box. Default proposal: store, on the encrypted volume like everything else, with a
purge-by-conversation command.

## Prompt format (implemented)

    Context from the user's personal archive (retrieved automatically, may be irrelevant):
    - [2026-04-12, image] two people at a cafe table, ruins visible behind
    - [2026-04-13, image] narrow street, scooters, laundry lines
    
    Using the context above only where it is actually relevant, answer:
    <the user's question>

Rules encoded there, deliberately: the model is told the context is retrieved and possibly
irrelevant (reduces confabulated connections); snippets carry dates and sources (the model can cite
"your photo from April 12" instead of vague claims); six snippets maximum (grounding, not noise).

## Budgets and priorities

- Retrieval: 3s hard budget, then bare passthrough. A person is waiting.
- Caption generation: background priority in oracled's queue; interactive chat preempts.
- Context size: 6 snippets, roughly 300 tokens , leaves the full window for the conversation.

## What is deliberately NOT here

- No cloud calls anywhere in the pipeline, including geocoding.
- No cross-account retrieval ever; the search schema is per-slot like everything else.
- No summarisation/consolidation daemon yet (tier 2 memories). That is its own design.
