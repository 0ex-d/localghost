# LocalGhost for Android

The Android client for LocalGhost — the on-device gateway into a memory layer that runs
on your own NAS. It captures the modalities a memory arrives in and connects the streams
that make up a life, ships the **originals** to your box, and surfaces the memory layer
back to you for reflection. Nothing transits a vendor cloud. The only cloud is you.

This app is the phone-side half. All remembering — extraction, clustering,
consolidation — happens in the daemon fleet under `server/`. The app captures, asks,
and displays. It never reasons over your data, and it never sends it anywhere but your
own NAS.

> Architecture of record: [`app/APP_ARCHITECTURE.md`](../APP_ARCHITECTURE.md)
> API it speaks: [`server/docs/INGEST_CONTRACT.md`](../../server/docs/INGEST_CONTRACT.md)

---

## What it does

**Capture (originals on device):**
- **Images & screenshots** → ghost.framed (journal entry, OCR, location)
- **Voice & audio** → ghost.voiced (transcribed, audio preserved)
- **Quick notes** → ghost.noted

**Connect (your other streams, read-only and revocable):**
- **Health** via Health Connect
- **Email** via Gmail/IMAP OAuth
- **Bank** via UK Open Banking (AISP) — added only once the trust model is hardened
- **Telegram** via official MTProto
- WhatsApp has no compliant read path; tracked as an open problem, not a feature.

**Reflect (the memory layer, back to you):**
- The reflection **queue** — questions the daemons surface, answered with
  **yes / no / don't ask again**. Opens when you want it; never nags.
- **Override** — open any memory, see how it was classified and why, reclassify, edit
  tags, mark an **anchor** (only you can), or delete.

---

## The non-negotiables

1. **Originals only.** The app never re-encodes, scales, or recompresses. It moves exact
   bytes; the NAS does all extraction. Re-saving strips EXIF — that is how a real GPS tag
   silently becomes `0,0`.
2. **Unredacted reads.** Capture uses `MediaStore` with `setRequireOriginal()` and the
   `ACCESS_MEDIA_LOCATION` permission. It does **not** use the system Photo Picker for
   geotagged media, because the Picker redacts location even when the permission is
   granted.
3. **Your box only.** Uploads go to your NAS over the local network or a WireGuard/
   Tailscale tunnel, authed with a per-device token. No third-party relay, no telemetry.
4. **Tokens stay home.** OAuth / Open Banking / Telegram credentials live in the Android
   Keystore, on the device and NAS trust boundary only.

---

## Permissions

| Permission | Why |
|---|---|
| `ACCESS_MEDIA_LOCATION` | Read **unredacted** EXIF/GPS. Without it, location silently returns `0,0`. Requested at runtime. |
| `READ_MEDIA_IMAGES` / `READ_MEDIA_AUDIO` | Read the camera roll and audio for capture. |
| `RECORD_AUDIO` | ghost.voiced live capture. |
| `INTERNET` | Upload to your NAS. |

Health, email, bank, and Telegram each request their own scoped, revocable grant at
connect time.

---

## How capture works

1. Enumerate items via `MediaStore`.
2. For each, call `MediaStore.setRequireOriginal(uri)` **before** `openInputStream` —
   this returns the original bytes with intact metadata.
3. Hash the raw bytes, check the local ledger, queue if new.
4. `WorkManager` uploads to the matching `/v1/ingest/*` endpoint under Wi-Fi/charging
   constraints, with retry and resume. The queue is idempotent and survives restarts.

The daemon returns the canonical memory record; the app stores the id and shows the
result.

---

## Build & run

> Status: scaffolding. The NAS-side `server/cmd/framed` pipeline (extraction, EXIF+XMP
> location, journal entries) is built and tested; this app is the capture/sync/reflect
> client in front of it.

```
app/android/        # Android Studio project (Kotlin)
```

Requirements: Android Studio, JDK 17+, a reachable LocalGhost NAS (local or via tunnel),
a device token from pairing.

1. Open `app/android` in Android Studio.
2. Set the NAS base URL and device token in the app's settings (or `local.properties`
   for dev).
3. Grant `ACCESS_MEDIA_LOCATION` when prompted — without it, geotags read as `0,0`.
4. Run on a device (location features need real hardware, not the emulator).

---

## Roadmap (risk gradient)

- **P0** — sync spine + ghost.framed (images) + ghost.noted (notes)
- **P1** — ghost.voiced (audio)
- **P2** — reflection queue + override + anchor + delete
- **P3** — Health Connect, email
- **P4** — Open Banking (after the token/consent model is hardened)
- **P5** — Telegram (MTProto)

---

## Why a native app at all

A browser tab cannot hold `ACCESS_MEDIA_LOCATION`, so any web upload of a phone photo
gets a location-redacted copy — coordinates arrive as `0,0`. The native app with the
permission and `setRequireOriginal()` is the only thing that gets the real original onto
your NAS. The capture sources are the easy part; the trust architecture is the product.
See `SECURITY.md`.