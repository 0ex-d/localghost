# TODO

Working list. Tick items off as they land, add new ones at the bottom of their section.
Started 2026-07-15, the night the box learned to repair itself on unlock.

## App , chat rendering and UX

- [x] **1. Markdown rendering in chat** , stdlib only. Range-based renderer, streaming-safe by
      construction (all parsing in remember/runCatching, plain-text fallback, fuzzed over 3485
      streaming prefixes). Includes SelectionContainer + per-message [ copy ]. Landed 2026-07-15.
- [ ] **2. Expandable thinking** , the `thinking… (n)` counter already carries the reasoning
      events; tap to expand and read the reasoning text, collapsed by default.
- [ ] **3. Auto-scroll during streaming** , follow the tail as tokens arrive; the moment the user
      scrolls up or holds, stop following; resume when they return to the bottom.
- [ ] **4. Keyboard inset bug** , the input box floats far above the keyboard. Almost certainly
      IME insets applied twice (window + composable padding); one has to go.

- [x] **18. Skip the app fingerprint after a recent device unlock** , gate key rebound with a
      10s auth-validity window (lockscreen unlock opens it); silent cipher path goes straight to
      the box PIN, prompt only outside the window. Landed 2026-07-15.
- [x] **19. Lock-screen links styled as buttons** ([ brackets ] + underline). Landed 2026-07-15.
- [x] **20. Scanner assembly burst** , 100ms sampling while capturing the rotating enrol frames
      (bounded burst, thermal tuning kept for indefinite hunting). Landed 2026-07-15.

## App/box , chat continuity

- [ ] **5. Chat disappeared after unlock** , conversation is saved on the box but the screen came
      back empty. On reconnect, re-adopt the active chatId and reload from /v1/chats/messages, or
      at minimum land in CHATS.

## Frames pipeline

- [ ] **6. Portrait photos render rotated** , preview/thumb derivation must apply the EXIF
      orientation transform (originals stay untouched). Plus a re-derive job for existing wrong
      thumbs.
- [ ] **7. Reprocess pass** , everything archived during the degraded window has missing frame
      records and failed search-ingest notifies. Run reprocess, verify the rebuild covers the
      notify gaps, watch the caption backlog drain on the GPU (resolves the "tags pending" state).

## Sync

- [ ] **8. Photos and videos sync concurrently** , decide the policy (probably sequential,
      photos first) and make the tail counter and the SYNC screen agree on totals.

## Server

- [ ] **9. Runtime bundle switchover** , bundle_db_runtime.sh --verify, then halt + unlock,
      confirm ps shows postgres from runtime/pgroot, decide on purging the OS packages. First
      real use of `halt`.
- [ ] **10. Drawer recents box-backed + chat rename endpoint.**
- [x] **11. Redis save policy** , `save 3600 1 60 20`: hourly snapshot for a quiet box, minute
      snapshots only under heavy write load (sync bursts, caption runs). Landed 2026-07-15.
      Verify live with `ghost-cli ghost.* / redis-cli CONFIG GET save` after next cold start.
- [ ] **12. Graceful shutdown on lock and redeploy** , the teardown must end everything
      GRACEFULLY, in order: (a) tell the daemons to stop taking new work and FINISH their
      in-flight items (framed mid-archive, searchd mid-ingest, oracled mid-generation , cancel or
      complete, never abandon); (b) give Redis an explicit save (BGSAVE/SHUTDOWN SAVE) so the
      last hour of state is on disk regardless of save thresholds; (c) pg_ctl stop in a mode
      that waits for clean checkpoint; (d) only then unmount. redeploy.sh rides this same path
      instead of relying on SIGTERM racing systemd , "restart tore the box down cleanly" must be
      a property, not a hope (tonight it wasn't: mounted-but-dead was the result).
- [ ] **13. notifyd alerting on service transitions; nobody-watches-watchd staleness flag;
      chat tok/s metric.**
- [ ] **14. Redis persistence at lock** , tie into item 12: explicit save before StopCache.

## Security / longer arc

- [ ] **15. Re-pair gating on FIDO2.**
- [ ] **16. Backup system** , weekly fulls + daily incremental diffs on the always-mounted HDD,
      asymmetric encryption (daily job never holds the key), folder unreachable by the app.
- [ ] **17. Box security threat model blog post** (separate from the Border Agent post).

## Done (this era)

- [x] Convergent unlock , mounted-but-dead is repairable; Warm gates only Unseal/Mount.
- [x] EnsureSchema bootstrap , pg_hba peer line for the run user; owner role, service roles,
      database, and ownership converged as superuser; grants (public + search) as owner.
- [x] phash column moved to the partitioned parent (ADD COLUMN on a partition is illegal).
- [x] `halt` command , maintenance stop, everything down, volume stays mounted, PIN-opaque.
- [x] Adopted-watchd support , idempotent start-cohort, socket-driven shutdown for orphans.
- [x] Binary ingest via temp+rename (no ETXTBSY against running binaries).
- [x] Spool ownership heal for legacy root-owned frames.
- [x] health.sh: /tmp-staged namespace entry; .stream sockets excluded from the roster.
- [x] bundle_db_runtime.sh: auto-nsenter + refuses to write to an unmounted path.
- [x] Blocking MODEL unlock stage , READY completes last, app lands on a box whose chat answers.
