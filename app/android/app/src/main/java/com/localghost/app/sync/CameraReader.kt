package com.localghost.app.sync

import android.content.ContentUris
import android.content.Context
import android.net.Uri
import android.provider.MediaStore
import java.io.InputStream

object CameraReader {

    data class Item(val uri: Uri, val name: String, val dateTaken: Long, val id: Long, val size: Long)

    private data class Cols(val collection: Uri, val id: String, val name: String,
                            val date: String, val bucket: String, val size: String)

    private fun cols(kind: MediaKind) = when (kind) {
        MediaKind.PHOTO -> Cols(
            MediaStore.Images.Media.EXTERNAL_CONTENT_URI, MediaStore.Images.Media._ID,
            MediaStore.Images.Media.DISPLAY_NAME, MediaStore.Images.Media.DATE_TAKEN,
            MediaStore.Images.Media.BUCKET_DISPLAY_NAME, MediaStore.Images.Media.SIZE)
        MediaKind.VIDEO -> Cols(
            MediaStore.Video.Media.EXTERNAL_CONTENT_URI, MediaStore.Video.Media._ID,
            MediaStore.Video.Media.DISPLAY_NAME, MediaStore.Video.Media.DATE_TAKEN,
            MediaStore.Video.Media.BUCKET_DISPLAY_NAME, MediaStore.Video.Media.SIZE)
    }

    private fun selectionArgs(c: Cols, after: Cursor) =
        "${c.bucket} = ? AND (${c.date} > ? OR (${c.date} = ? AND ${c.id} > ?))" to
        arrayOf("Camera", after.dateTaken.toString(), after.dateTaken.toString(), after.id.toString())

    /** Returns (item count, total bytes) so the UI can show a real ETA, not just an item counter. */
    fun count(ctx: Context, kind: MediaKind, after: Cursor): Pair<Int, Long> {
        val c = cols(kind)
        val (sel, args) = selectionArgs(c, after)
        ctx.contentResolver.query(c.collection, arrayOf(c.id, c.size), sel, args, null)?.use { cur ->
            val sizeCol = cur.getColumnIndexOrThrow(c.size)
            var n = 0; var bytes = 0L
            while (cur.moveToNext()) { n++; bytes += cur.getLong(sizeCol) }
            return n to bytes
        }
        return 0 to 0L
    }

    fun syncFrom(
        ctx: Context,
        kind: MediaKind,
        after: Cursor,
        send: (Item, InputStream) -> Boolean,
        alreadyHave: (Item) -> Boolean,
        shouldAbort: () -> Boolean,
        onItemStart: (Item) -> Unit,
        onBytes: (read: Long) -> Unit,
        onProgress: (sent: Int) -> Unit,
    ): CommandResult {
        val c = cols(kind)
        val projection = arrayOf(c.id, c.name, c.date, c.size)
        val (sel, args) = selectionArgs(c, after)
        val sort = "${c.date} ASC, ${c.id} ASC"

        var sent = 0
        var failed = 0
        var skippedExisting = 0
        var bytes = 0L
        try {
            ctx.contentResolver.query(c.collection, projection, sel, args, sort)?.use { cur ->
                val idCol = cur.getColumnIndexOrThrow(c.id)
                val nameCol = cur.getColumnIndexOrThrow(c.name)
                val dateCol = cur.getColumnIndexOrThrow(c.date)
                val sizeCol = cur.getColumnIndexOrThrow(c.size)
                while (cur.moveToNext()) {
                    val id = cur.getLong(idCol)
                    val base = ContentUris.withAppendedId(c.collection, id)
                    val uri = if (kind == MediaKind.PHOTO) MediaStore.setRequireOriginal(base) else base
                    val item = Item(uri, cur.getString(nameCol) ?: "unknown",
                        cur.getLong(dateCol), id, cur.getLong(sizeCol))

                    // Cooperative pause: checked PER ITEM, not just at run start. Before this, tapping
                    // PAUSE during a long sweep did nothing visible , the flag only gated the NEXT
                    // run, and a full-roll pass kept going for hours. Aborting between items is safe:
                    // the cursor sits at the last confirmed item, so resume continues exactly there.
                    if (shouldAbort()) {
                        android.util.Log.i("LocalGhost", "sync $kind paused mid-run after $sent items")
                        return CommandResult(Stream.CAMERA, kind, sent, bytes)
                    }
                    onItemStart(item)
                    // PRE-UPLOAD EXISTENCE CHECK: hash the file locally (a local read , seconds for a
                    // video) and ask the box before streaming it (minutes for a video). alreadyHave
                    // returns false on ANY doubt , hash failure, check failure , so uncertainty
                    // always falls through to a real upload; skipping only ever happens on the box's
                    // explicit confirmation. A confirmed-existing item counts as sent: it IS on the
                    // box, and the cursor should advance past it exactly as if just uploaded.
                    if (alreadyHave(item)) {
                        sent++
                        onProgress(sent)
                        skippedExisting++
                        continue
                    }
                    val ok = ctx.contentResolver.openInputStream(item.uri)?.use { stream ->
                        val counting = CountingStream(stream, onBytes)
                        val confirmed = send(item, counting)
                        if (confirmed) bytes += counting.count
                        confirmed
                    } ?: false

                    if (ok) {
                        sent++
                        onProgress(sent)
                    } else {
                        // A single item failing (transient 503, network blip, one unreadable file) must NOT
                        // abort the whole batch , the old `break` here stopped the entire photo sync at the
                        // first hiccup (e.g. 10/2943 then it gave up). Skip this one and keep going; its
                        // cursor was not advanced (see the caller), so it is retried on the next run.
                        failed++
                        android.util.Log.w("LocalGhost", "sync ${kind}: item ${item.name} not confirmed, skipping (retried next run)")
                    }
                }
            }
        } catch (e: Exception) {
            return CommandResult(Stream.CAMERA, kind, sent, bytes, error = e.message)
        }
        if (skippedExisting > 0) android.util.Log.i("LocalGhost", "sync $kind: $skippedExisting already on box, upload skipped")
        if (failed > 0) android.util.Log.i("LocalGhost", "sync $kind: $sent sent, $failed skipped (will retry)")
        return CommandResult(Stream.CAMERA, kind, sent, bytes)
    }

    /** Content hash in EXACTLY framed's format , hex(sha256(bytes)[:16]), 32 lowercase hex chars ,
     *  so a phone-side hash matches the box's archive identity byte for byte. Null on read failure
     *  (callers treat null as "not known to exist" and upload). */
    fun hashOf(ctx: Context, item: Item): String? = try {
        ctx.contentResolver.openInputStream(item.uri)?.use { stream ->
            val md = java.security.MessageDigest.getInstance("SHA-256")
            val buf = ByteArray(64 * 1024)
            while (true) {
                val n = stream.read(buf)
                if (n < 0) break
                md.update(buf, 0, n)
            }
            md.digest().copyOfRange(0, 16).joinToString("") { b -> "%02x".format(b) }
        }
    } catch (e: Exception) {
        android.util.Log.w("LocalGhost", "hash of ${item.name} failed: ${e.message}")
        null
    }

    private class CountingStream(
        private val inner: InputStream,
        private val onBytes: (Long) -> Unit,
    ) : InputStream() {
        var count = 0L; private set
        override fun read(): Int = inner.read().also { if (it >= 0) { count++; onBytes(count) } }
        override fun read(b: ByteArray, off: Int, len: Int): Int =
            inner.read(b, off, len).also { if (it > 0) { count += it; onBytes(count) } }
        override fun close() = inner.close()
    }
}
