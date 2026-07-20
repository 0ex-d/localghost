# The data model

One Postgres database on the encrypted volume, one schema, converged by `EnsureSchema` at every
unlock. The design rule, stated once and enforced everywhere: **read paths touch one table, or one
table plus one batched lookup. No joins to answer a screen.** We trade normalisation for that
gladly , display fields are denormalized onto their row, soft references replace foreign keys, and
each table has exactly one writing daemon.

## The entity map

```
INGESTION (each daemon writes its own tables, nobody else's)

 ghost.framed                ghost.noted               ghost.tallyd
 ┌─────────────┐             (files -> journal)        ┌────────────────┐
 │ frames      │──hash──┐                              │ health_metrics │ (day,metric) PK
 │  place TEXT │        │                              │ health_samples │ (metric,ts) PK
 │  display_   │        │                              └────────────────┘
 │  name TEXT  │        │
 ├─────────────┤        │
 │ frame_tags  │<───────┘  write model: append-only with 'user_removed' tombstones;
 │ points      │           reads use the two-query pattern, never a join
 │ geo_points  │           (imported once; resolver reads, nothing else touches)
 │ geo_names   │
 └─────────────┘

 THE ONE SHARED SEAM , the journal (every ingester writes, ONLY synthd consumes)

 ┌──────────────────────────────────────────────────────────┐
 │ journal_entries   UNIQUE(source, ref) = idempotent       │
 │   framed: "photo at <place>"    ref = frame hash         │
 │   noted:  texts, emails, chats  ref = content hash/chat: │
 │   tallyd: daily health line     ref = health:<day>       │
 │   distilled BOOL = synthd's high-water mark              │
 └──────────────────────────────────────────────────────────┘
                          │ distillation (oracled)
                          v
 ┌──────────────────────────────────────────────────────────┐
 │ memories   kind distilled|user, user_edited, tombstoned  │
 │   source_ref TEXT (soft ref to journal), source_chat     │
 │ reports    day 'MM-DD' PK , On This Day cache            │
 └──────────────────────────────────────────────────────────┘

 CONVERSATION & APP SURFACE (secd writes chats/messages/notifications; synthd writes memories)

 chats ── chat_messages (chat_id soft ref, index (chat_id,id))
 notifications
```

## Access paths and their cost (the contract each screen holds)

| Screen / caller        | Query shape                                                | Joins |
|------------------------|------------------------------------------------------------|-------|
| Gallery list           | frames by taken_at DESC, then ONE batched frame_tags IN()  | 0     |
| Gallery search         | frames + EXISTS(frame_tags) per term                       | 0*    |
| Map dots / day tracks  | frames w/ GPS; paths/*.geojson straight off disk           | 0     |
| Chat history           | chats; chat_messages by (chat_id, id) keyset               | 0     |
| Memories screen        | memories WHERE NOT tombstoned                              | 0     |
| Memory injection       | memories ILIKE per term, scored in Go                      | 0     |
| Day summary / check-in | frames range + journal range + health_metrics day = 3 SELECTs | 0  |
| On This Day            | frames to_char scan + journal to_char scan, cached in reports | 0  |
| Health screen          | health_metrics >= since, one ordered scan                  | 0     |
| synthd distill poll    | journal_entries WHERE NOT distilled (partial index)        | 0     |
| Geocoder               | geo_points bbox via lat/lon btrees; geo_names PK gets      | 0     |

*EXISTS subquery, not a join , stops at first match per frame.

## The rules that keep it this way

1. **Single writer per table.** framed writes frames/frame_tags/points/geo, noted+tallyd+framed
   write journal_entries (their own rows), synthd writes memories/reports, secd writes
   chats/messages/notifications. A daemon never updates another daemon's table.
2. **Denormalize display fields onto the row.** frames.place and frames.display_name are copies
   of derivable data, refreshed by the writer when inputs change (reprocess, tag edits). The read
   path never reconstructs them.
3. **Soft references, no FK constraints.** memories.source_ref names a journal row; chat_messages
   .chat_id names a chat. Nothing enforces them , daemons start in any order, imports arrive out
   of order, and a dangling ref costs a skipped row, not a failed insert.
4. **Tombstones over deletes** wherever the user overrides the model (frame_tags user_removed,
   memories.tombstoned). The person's correction must survive every future model pass.
5. **Idempotent writers.** Natural keys everywhere: frames.hash, journal (source,ref),
   health (day,metric) / (metric,ts), geo geonameid, reports.day. Re-running any import is a no-op.
6. **Two-query pattern for one-to-many display data** (frames -> tags): page the parent, then one
   IN() lookup for children. Never a join in the hot query, never N+1.

## Known scans, accepted at current scale, with named upgrade paths

- On This Day filters with to_char() , a full frames scan, fine to ~1M frames, cached in reports
  regardless. Upgrade: STORED generated column mmdd + index, one ALTER, zero code changes.
- memoriesSource uses ILIKE over all live memories , fine to ~50k memories. Upgrade: embeddings
  into memories.emb (reserved) and semantic ranking (TODO 30c).
- Geocoder candidate sets ride the lat/lon btrees, ordered by approximate squared-degree
  distance so LIMIT keeps the closest candidates even in dense areas; exact haversine picks in Go.

## The geo lifecycle (how it gets there, how it gets updated)

1. **Arrival.** New boxes: setup runs fetch_geo.sh onto the volume and imports while the
   provisioned Postgres is up , the geocoder is live from photo one. Existing boxes: drop the
   files in <mount>/geo, `ghost-cli ghost.framed geo-import`, `reprocess`.
2. **Resolution.** frames.place is a SNAPSHOT taken at archive/reprocess time , deliberate
   denormalization (rule 2), so geocoding cost is paid once per frame, not per read.
3. **Update.** GeoNames ships daily dumps. `GHOST_GEO_REFRESH=1 tools/fetch_geo.sh <mount>/geo`
   re-downloads; `geo-import` UPSERTS by geonameid (renames and moves land; deletions linger ,
   named limit); places on existing frames refresh only via `reprocess`, by design , an
   unprompted mass place-rewrite would surprise the person. Cadence: yearly is plenty; place
   names do not move often.
