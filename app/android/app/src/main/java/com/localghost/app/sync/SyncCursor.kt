package com.localghost.app.sync

import android.content.Context

/**
 * The phone-side sync position, per media kind. The box does not yet track per-device cursors (its
 * report endpoint is a stub), so the phone remembers the (dateTaken, id) of the newest item it has
 * CONFIRMED uploaded and only offers newer ones next run. Confirmed means secd answered 202 , a
 * network blip mid-photo leaves the cursor untouched, so that photo is retried on the next run.
 *
 * When the box grows real per-device tracking, this becomes the local cache of the box's answer
 * rather than the source of truth; the shape stays the same.
 */
object SyncCursor {
    private fun prefs(ctx: Context) = ctx.getSharedPreferences("localghost.sync.cursor", Context.MODE_PRIVATE)

    fun get(ctx: Context, kind: MediaKind): Cursor {
        val p = prefs(ctx)
        return Cursor(p.getLong("${kind.wire}.ts", 0L), p.getLong("${kind.wire}.id", 0L))
    }

    /** Advance past a confirmed item. Monotonic , an out-of-order confirm never moves the cursor back. */
    fun advance(ctx: Context, kind: MediaKind, dateTaken: Long, id: Long) {
        val cur = get(ctx, kind)
        if (dateTaken < cur.dateTaken || (dateTaken == cur.dateTaken && id <= cur.id)) return
        prefs(ctx).edit()
            .putLong("${kind.wire}.ts", dateTaken)
            .putLong("${kind.wire}.id", id)
            .apply()
    }

    /** Forget the position (e.g. after a box wipe or re-enrol , everything re-syncs). */
    fun reset(ctx: Context, kind: MediaKind) {
        prefs(ctx).edit().remove("${kind.wire}.ts").remove("${kind.wire}.id").apply()
    }
}
