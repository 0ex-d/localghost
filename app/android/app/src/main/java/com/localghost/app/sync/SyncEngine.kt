package com.localghost.app.sync

import android.content.Context
import com.localghost.app.net.BoxClient
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

class SyncEngine(private val ctx: Context) {

    interface Progress {
        fun onStart(kind: MediaKind, total: Int, totalBytes: Long)
        fun onItemStart(kind: MediaKind, name: String, index: Int, total: Int, size: Long)
        fun onItemBytes(kind: MediaKind, read: Long, size: Long, runBytesSent: Long, speedBps: Double, etaSeconds: Long)
        fun onItemDone(kind: MediaKind, sent: Int, total: Int)
        fun onDone(result: CommandResult)
    }

    suspend fun runCamera(kind: MediaKind, progress: Progress) {
        when (val cmd = BoxClient.nextCameraCommand(ctx, kind)) {
            is Command.SyncCamera -> exec(cmd, progress)
            else -> { progress.onStart(kind, 0, 0L); progress.onDone(CommandResult(Stream.CAMERA, kind, 0, 0)) }
        }
    }

    private suspend fun exec(cmd: Command.SyncCamera, progress: Progress) {
        // WHERE WAS I: ask the box for the newest taken_at it has archived for this kind, and fast-
        // forward the local cursor to it if the box is ahead (killed app, reinstall, cursor loss). The
        // local cursor stays authoritative when it is ahead (uploads the box has not processed yet).
        val (photoMs, videoMs) = BoxClient.framesLatest(ctx)
        val boxMs = if (cmd.kind == MediaKind.PHOTO) photoMs else videoMs
        val after = if (boxMs > cmd.after.dateTaken) {
            android.util.Log.i("LocalGhost", "sync ${cmd.kind}: box is ahead (box=$boxMs local=${cmd.after.dateTaken}), fast-forwarding cursor")
            SyncCursor.advance(ctx, cmd.kind, boxMs, 0)
            Cursor(boxMs, 0)
        } else cmd.after
        val (total, totalBytes) = withContext(Dispatchers.IO) { CameraReader.count(ctx, cmd.kind, after) }
        // The one line that explains a "0 items" sync: was the camera roll EMPTY after the cursor
        // (query/permission/cursor issue), or full but nothing CONFIRMED (upload issue)? Both look
        // identical on screen without this.
        android.util.Log.i("LocalGhost", "sync ${cmd.kind}: $total items ($totalBytes bytes) after cursor (${after.dateTaken},${after.id})")
        progress.onStart(cmd.kind, total, totalBytes)
        val meter = SyncMeter(totalBytes)
        var index = 0
        var curSize = 0L
        var doneBytes = 0L   // bytes from items already CONFIRMED sent this run
        var curRead = 0L
        var sawFailure = false // once an item fails, freeze the cursor so the gap is retried next run
        val result = withContext(Dispatchers.IO) {
            CameraReader.syncFrom(
                ctx, cmd.kind, after,
                shouldAbort = { com.localghost.app.settings.AppSettings.syncPaused(ctx) },
                alreadyHave = { item ->
                    // Hash locally, ask the box. False on ANY doubt (hash failure, request failure ,
                    // framesHave already returns empty on error) so uncertainty uploads. A confirmed
                    // hit advances the cursor below via the same contiguous-streak rule: the send
                    // lambda is never called for it, so advance here under the same conditions.
                    val h = CameraReader.hashOf(ctx, item)
                    val onBox = h != null &&
                        kotlinx.coroutines.runBlocking { BoxClient.framesHave(ctx, listOf(h)) }.contains(h)
                    if (onBox && !sawFailure) SyncCursor.advance(ctx, cmd.kind, item.dateTaken, item.id)
                    onBox
                },
                send = { item, stream ->
                    val ok = kotlinx.coroutines.runBlocking { BoxClient.ingest(ctx, cmd.kind, item.name, stream, item.dateTaken) }
                    if (!ok) sawFailure = true
                    // Advance the cursor only while the run is still a CONTIGUOUS success streak. Items
                    // upload oldest-first; if #11 failed, we must not advance past it even though #12+
                    // succeed, or #11 would fall behind the cursor and never retry. Once anything has
                    // failed this run, stop advancing , the next run resumes at the first gap.
                    if (ok && !sawFailure) SyncCursor.advance(ctx, cmd.kind, item.dateTaken, item.id)
                    ok
                },
                onItemStart = { item ->
                    index++; curSize = item.size; curRead = 0L
                    progress.onItemStart(cmd.kind, item.name, index, total, item.size)
                },
                onBytes = { read ->
                    curRead = read
                    val (bps, eta) = meter.update(doneBytes + read)
                    progress.onItemBytes(cmd.kind, read, curSize, doneBytes + read, bps, eta)
                },
                onProgress = { sent -> doneBytes += curSize; progress.onItemDone(cmd.kind, sent, total) },
            )
        }
        android.util.Log.i("LocalGhost", "sync ${cmd.kind} finished: ${result.itemsSent} confirmed of $total")
        BoxClient.report(result)
        progress.onDone(result)
    }
}
