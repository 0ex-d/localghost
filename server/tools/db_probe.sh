#!/usr/bin/env bash
# db_probe.sh , what the box's own Postgres (the one on the volume, not the host's) is doing, and
# how long the Box Status screen's query takes. Read-only: it looks at pg_stat_activity, table
# sizes and indexes, and runs the Status query under EXPLAIN ANALYZE (a SELECT) with a 120 s cap.
#
# Written 29 Sep 2026: opening Box Status froze the database for the app, the phone then showed
# the unlock screen, and the unlock sat in "loading database" behind it.
#
#   sudo ./tools/db_probe.sh           # once
#   sudo ./tools/db_probe.sh --watch   # also: what is running, every 5 s for a minute (open Status now)
set -u
[ "$(id -u)" -eq 0 ] || { echo "run as root (sudo)" >&2; exit 1; }
WATCH=0; [ "${1:-}" = "--watch" ] && WATCH=1
PID="$(pidof ghost.secd || true)"; PID="${PID%% *}"
[ -n "$PID" ] || { echo "ghost.secd is not running" >&2; exit 1; }
R="/proc/$PID/root"; M="/var/lib/ghost/mnt/slot0"
SC="$R$M/services.conf"
[ -f "$SC" ] || { echo "no services.conf on the volume , is the box unlocked?" >&2; exit 1; }
PGB="$(tr -d '\n' < "$SC" | sed -n 's/.*"postgres"[[:space:]]*:[[:space:]]*{\([^}]*\)}.*/\1/p')"
PORT="$(printf '%s' "$PGB" | sed -n 's/.*"port"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p')"; PORT="${PORT:-6000}"
DB="$(printf '%s' "$PGB" | sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"; DB="${DB:-ghost}"
RUNAS="$(stat -c %U "$R$M/postgres")" # the data dir's owner is the superuser (peer auth on the socket)
q() {
    if [ -n "${DBPROBE_PSQL:-}" ]; then $DBPROBE_PSQL "$@"; return; fi # tests
    nsenter -t "$PID" -m -- runuser -u "$RUNAS" -- psql -X -q -h "$M/postgres" -p "$PORT" -d "$DB" -P pager=off "$@"
}
echo "== the volume's Postgres: db $DB, port $PORT, as $RUNAS"

NOW_SQL="
SET statement_timeout = '20s';
SELECT pid, date_trunc('second', now()-xact_start) AS in_tx, date_trunc('second', now()-query_start) AS running,
       state, coalesce(wait_event_type||':'||wait_event, '') AS waiting, pg_blocking_pids(pid) AS blocked_by,
       left(regexp_replace(query, '\s+', ' ', 'g'), 110) AS query
FROM pg_stat_activity
WHERE backend_type = 'client backend' AND pid <> pg_backend_pid()
  AND (state <> 'idle' OR xact_start IS NOT NULL)
ORDER BY xact_start NULLS LAST, query_start;"

echo "== running now (anything not idle, and any open transaction)"
q -c "$NOW_SQL"

echo "== the tables the Status screen counts"
q <<'SQL'
SET statement_timeout = '30s';
SELECT n.nspname||'.'||c.relname AS "table", c.reltuples::bigint AS rows_est,
       pg_size_pretty(pg_total_relation_size(c.oid)) AS size,
       s.n_dead_tup AS dead, s.last_autoanalyze::timestamp(0) AS analyzed
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_stat_all_tables s ON s.relid = c.oid
WHERE c.relkind = 'r' AND (n.nspname, c.relname) IN (('public','frames'),('public','frame_tags'),('search','jobs'),('public','daemon_state'))
ORDER BY 1;
SELECT tablename, indexname, indexdef FROM pg_indexes
WHERE schemaname = 'public' AND tablename IN ('frames','frame_tags') AND indexdef ILIKE '%(hash%' ORDER BY 1, 2;
SELECT table_name, column_name, data_type FROM information_schema.columns
WHERE table_schema = 'public' AND table_name IN ('frames','frame_tags') AND column_name = 'hash';
SQL

echo "== the Status screen's queries, timed (the stage count is the SQL this box's secd runs for /v1/pipeline;"
echo "   the new drop reads frame_tags once instead, same counts)"
q <<'SQL'
SET statement_timeout = '120s';
\timing on
EXPLAIN (ANALYZE, BUFFERS, COSTS OFF)
WITH ver AS (SELECT coalesce(max(pipe_ver), 0) AS v FROM frames)
SELECT (SELECT v FROM ver),
       count(*) FILTER (WHERE kind = 'photo'),
       count(*) FILTER (WHERE kind = 'video'),
       count(*) FILTER (WHERE kind NOT IN ('photo','video')),
       count(*) FILTER (WHERE staged AND pipe_ver >= (SELECT v FROM ver)),
       count(*) FILTER (WHERE staged AND preview_path <> '' AND thumb_path <> ''),
       count(*) FILTER (WHERE staged AND description <> ''),
       count(*) FILTER (WHERE staged AND display_name <> ''),
       count(*) FILTER (WHERE staged AND tagged),
       count(*) FILTER (WHERE staged AND tagged AND categorised),
       count(*) FILTER (WHERE staged AND pipe_ver >= (SELECT v FROM ver) AND preview_path <> '' AND thumb_path <> ''
                          AND description <> '' AND display_name <> '' AND tagged AND categorised),
       count(*) FILTER (WHERE staged AND description <> '' AND described_at >= extract(epoch FROM now())::bigint - 3600),
       count(*) FILTER (WHERE staged AND description <> '' AND described_at >= extract(epoch FROM now())::bigint - 86400),
       coalesce(max(described_at), 0)
FROM (SELECT f.*, f.kind IN ('photo','video') AS staged,
             EXISTS (SELECT 1 FROM frame_tags t WHERE t.hash = f.hash) AS tagged,
             NOT EXISTS (SELECT 1 FROM frame_tags t WHERE t.hash = f.hash AND t.category = '' AND t.source <> 'user_removed') AS categorised
      FROM frames f) x;
SELECT count(*) FILTER (WHERE kind = 'caption' AND attempts < 5) AS captions, count(*) FILTER (WHERE kind = 'tag' AND attempts < 5) AS tags,
       count(*) FILTER (WHERE kind = 'embed_text' AND attempts < 5) AS embeds, count(*) FILTER (WHERE attempts >= 5) AS parked, count(*) AS all_jobs
FROM search.jobs;
SELECT * FROM search.health;
SQL

if [ "$WATCH" = 1 ]; then
    echo "== watching for a minute: open Box Status on the phone now"
    for i in $(seq 1 12); do
        echo "-- $(date +%T)"
        q -t -c "$NOW_SQL" | grep -v "^$"
        sleep 5
    done
fi
