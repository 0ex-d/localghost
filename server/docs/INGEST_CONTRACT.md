# LocalGhost Ingest Contract (v1)

The interface between the app (`app/*`) and the daemon fleet (`server/cmd/*`). The app
captures and connects sources; it POSTs originals and connector payloads here. The
daemons extract, cluster, and write into the shared memory layer, and return the
canonical record. This contract is the seam — pin it before building either side.

Module: `github.com/LocalGhostDao/localghost/server`

---

## Transport & auth

- **Local-first.** The app reaches the NAS on the local network at home, or over a
  WireGuard/Tailscale tunnel when away. Never a third-party relay.
- **TLS** terminated at the existing nginx in front of the node.
- **Per-device token**, issued once at pairing, sent on every request:
  ```
  Authorization: Bearer <device-token>
  ```
- A revoked token is rejected with `401`. Revocation is immediate and per-device.

Base path: `/v1`. Breaking changes bump to `/v2`.

---

## Common envelope

Every ingest call carries context the NAS cannot derive. For JSON bodies these are
top-level fields; for binary uploads they are multipart fields or `X-` headers.

| Field | Required | Meaning |
|---|---|---|
| `device_id` | yes | Stable per-device id. |
| `timezone` | yes | IANA tz of the device, e.g. `Europe/London`. Resolves zoneless EXIF time. |
| `captured_at` | when known | Original capture time (RFC3339, or raw EXIF for images). |
| `content_hash` | for binary | SHA-256 of the **raw** bytes. Drives idempotency. |
| `source` | yes | Modality: `image|audio|note|transactions|health|email|messages`. |

### Idempotency

The app must set `content_hash` (binary) or an `Idempotency-Key` header (connectors).
The server dedupes on it:

- new content → `201 Created` + record
- already ingested → `200 OK` + the existing record (no reprocessing)

This makes sync safe to retry and resumable.

---

## Canonical memory record

Every ingest returns the record the daemon wrote. Fields are modality-agnostic; absent
ones are omitted.

```json
{
  "id": "mem_01J...",
  "modality": "image",
  "source_ref": "/data/originals/2026/06/IMG_0001.jpg",
  "captured_at": "2026-06-18T15:58:34",
  "timezone": "Europe/London",
  "processed_at": "2026-06-18T16:00:11Z",

  "entry": "…",
  "summary": "…",
  "text_in_image": "…",
  "transcription": "…",
  "people": [], "places": [], "tags": [],

  "location": {
    "coordinates": "0.512300, -0.118200",
    "lat": "0.512300", "lon": "-0.118200",
    "altitude_m": "12.4",
    "gps_time_utc": "2026:06:18 14:58:32",
    "gps_source": "exif|xmp",
    "place_name": "Woolwich, London"
  },

  "cluster_id": "clu_…",
  "anchor": false,
  "significance_guess": "ordinary|notable|anchor-candidate",
  "exif": { "Make": "samsung", "Model": "Galaxy S26 Ultra" }
}
```

`source_ref` points at the preserved original on the NAS — the original never moves;
the record is a handle on it. `anchor` is set only by the user (see PATCH). `gps_source`
records whether coordinates came from EXIF or the XMP fallback.

---

## Ingest endpoints

### `POST /v1/ingest/image` → ghost.framed
`multipart/form-data`: `file` (raw jpeg/png bytes, unmodified), `meta` (envelope JSON).
Daemon extracts journal entry, OCR text, people/places/tags, reads EXIF+XMP location.
→ memory record.

### `POST /v1/ingest/audio` → ghost.voiced
`multipart/form-data`: `file` (raw wav/m4a), `meta`. Daemon transcribes, preserves audio.
→ record with `transcription`.

### `POST /v1/ingest/note` → ghost.noted
`application/json`: envelope + `{ "text": "…" }`. → record.

### `POST /v1/ingest/transactions` → ghost.tallyd
`application/json`: envelope + Open Banking transactions array (read-only AISP payload).
→ array of records (one per material transaction the daemon keeps).

### `POST /v1/ingest/health` → ghost.<health>
`application/json`: envelope + Health Connect records (steps, sleep, workouts, mood).
→ records.

### `POST /v1/ingest/email` → ghost.<mail>
`multipart/form-data`: `message` (RFC822 or parsed JSON), `attachments[]`, `meta`.
→ records (booking, receipt, etc. — the memory-worthy signal, originals preserved).

### `POST /v1/ingest/messages` → ghost.<msg>
`application/json` or `multipart`: a Telegram (MTProto) export segment.
→ records. (WhatsApp has no compliant ingest path yet — see app spec.)

---

## Reflection & override

The pull/push half of the memory layer (POST_09). All require the device token.

### `GET /v1/queue`
Pending reflection questions the daemons want answered.
```json
{ "questions": [
  { "id": "q_…", "from": "ghost.shadowd",
    "prompt": "Are these five notes one cluster worth naming?",
    "memory_ids": ["mem_…","mem_…"] }
] }
```

### `POST /v1/queue/{id}/answer`
`{ "answer": "yes" | "no" | "never" }` — the three buttons. `never` = don't ask again.

### `GET /v1/memory/{id}`
The memory plus its interpretation layer (cluster, why it was classified as it was).

### `PATCH /v1/memory/{id}`
User override. Any subset of:
```json
{ "tags": [...], "place_name": "…", "anchor": true, "cluster_id": "…", "entry": "…" }
```
`anchor` can only be set here, by the user — no daemon infers it.

### `DELETE /v1/memory/{id}`
Deletes the original and the record, and **enqueues** a background restructure:
dependent clusters re-form, citing summaries regenerate. Returns `202 Accepted` — the
rewrite is slow by design (the consolidation pass), not synchronous.

---

## Errors

```json
{ "error": { "code": "unauthorized|bad_request|too_large|unsupported_media|server_error",
             "message": "human-readable" } }
```

| Status | When |
|---|---|
| `200` | OK / already ingested (idempotent hit) |
| `201` | New record created |
| `202` | Accepted, async (delete restructure) |
| `400` | Malformed envelope or body |
| `401` | Missing/revoked token |
| `413` | Payload over the node's size limit |
| `415` | Unsupported media type |
| `5xx` | Daemon/model failure — app should retry per its queue policy |

---

## Notes for implementers

- The app sends **raw originals only**. No on-device re-encoding (it strips EXIF/GPS).
- Coordinates of exactly `0,0` are treated as absent (silent-redaction guard).
- EXIF `DateTimeOriginal` is zoneless: resolve with `gps_time_utc` if present, else the
  envelope `timezone`.
- Daemons own all extraction and reasoning. The contract is deliberately thin: bytes in,
  canonical record out.