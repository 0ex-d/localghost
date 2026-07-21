package com.localghost.app.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.localghost.app.net.BoxClient
import com.localghost.app.ui.theme.*

/**
 * Full-screen photo viewer with pinch-zoom and pan , the box's own preview JPEG, fetched on open,
 * never cached to disk (the archive is the box's job; the phone is a window, not a second copy).
 * Double-tap toggles fit/3x, single tap dismisses, drag pans while zoomed.
 */
@Composable
fun ImageViewer(hash: String, caption: String = "", onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    var bmp by remember(hash) { mutableStateOf<android.graphics.Bitmap?>(null) }
    var failed by remember(hash) { mutableStateOf(false) }
    var quality by remember(hash) { mutableStateOf("") }
    LaunchedEffect(hash) {
        // ORIGINAL first , the untouched archive bytes (the phone can decode jpeg/heic/png
        // natively). Preview then thumb are FALLBACKS for originals the codec refuses, not the
        // ceiling. The quality tag says which one you are looking at , no silent downgrades.
        for ((label, bytes) in listOf(
            "original" to BoxClient.frameOriginal(ctx, hash),
            "preview" to BoxClient.framePreview(ctx, hash),
            "thumb" to BoxClient.frameThumb(ctx, hash))) {
            if (bytes == null) continue
            val b = decodeUpright(bytes)
            if (b != null) {
                bmp = b
                quality = label
                return@LaunchedEffect
            }
        }
        failed = true
    }
    var scale by remember(hash) { mutableStateOf(1f) }
    var offX by remember(hash) { mutableStateOf(0f) }
    var offY by remember(hash) { mutableStateOf(0f) }
    Dialog(onDismissRequest = onDismiss,
        properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Box(Modifier.fillMaxSize().background(Color.Black), contentAlignment = Alignment.Center) {
            when {
                bmp != null -> Image(
                    bitmap = bmp!!.asImageBitmap(),
                    contentDescription = null,
                    contentScale = ContentScale.Fit,
                    modifier = Modifier.fillMaxSize()
                        .graphicsLayer(scaleX = scale, scaleY = scale,
                            translationX = offX, translationY = offY)
                        .pointerInput(hash) {
                            detectTransformGestures { _, pan, gz, _ ->
                                scale = (scale * gz).coerceIn(1f, 12f)
                                if (scale > 1f) { offX += pan.x; offY += pan.y }
                                else { offX = 0f; offY = 0f }
                            }
                        }
                        .pointerInput(hash) {
                            detectTapGestures(
                                onDoubleTap = {
                                    if (scale > 1.2f) { scale = 1f; offX = 0f; offY = 0f } else scale = 3f
                                },
                                onTap = { if (scale <= 1.05f) onDismiss() })
                        })
                failed -> Text("! could not load this photo from the box",
                    color = TerminalDim, style = MaterialTheme.typography.bodyMedium)
                else -> Text("loading…", color = GhostTextDim,
                    style = MaterialTheme.typography.bodyMedium)
            }
            if (caption.isNotBlank() && scale <= 1.05f) {
                Text(caption, color = GhostTextDim,
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.align(Alignment.BottomStart)
                        .background(Color(0xCC000000)).padding(12.dp))
            }
            Text("[ close ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.align(Alignment.TopEnd).padding(16.dp).clickable { onDismiss() })
            if (quality.isNotEmpty() && quality != "original" && scale <= 1.05f) {
                Text("showing $quality , original would not decode on this phone",
                    color = TerminalDim, style = MaterialTheme.typography.labelSmall,
                    modifier = Modifier.align(Alignment.TopStart).padding(16.dp))
            }
        }
    }
}

/** Decode honouring EXIF orientation , BitmapFactory ignores the tag, so ORIGINALS (unlike the
 *  box's pre-uprighted thumbs) arrived sideways. The framework ExifInterface reads the tag from
 *  the bytes (no extra dependency); one Matrix applies rotation and mirroring. */
private fun decodeUpright(bytes: ByteArray): android.graphics.Bitmap? {
    val b = android.graphics.BitmapFactory.decodeByteArray(bytes, 0, bytes.size) ?: return null
    val o = try {
        android.media.ExifInterface(java.io.ByteArrayInputStream(bytes))
            .getAttributeInt(android.media.ExifInterface.TAG_ORIENTATION,
                android.media.ExifInterface.ORIENTATION_NORMAL)
    } catch (_: Exception) { android.media.ExifInterface.ORIENTATION_NORMAL }
    if (o == android.media.ExifInterface.ORIENTATION_NORMAL || o == 0) return b
    val mx = android.graphics.Matrix()
    when (o) {
        android.media.ExifInterface.ORIENTATION_ROTATE_90 -> mx.postRotate(90f)
        android.media.ExifInterface.ORIENTATION_ROTATE_180 -> mx.postRotate(180f)
        android.media.ExifInterface.ORIENTATION_ROTATE_270 -> mx.postRotate(270f)
        android.media.ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> mx.postScale(-1f, 1f)
        android.media.ExifInterface.ORIENTATION_FLIP_VERTICAL -> mx.postScale(1f, -1f)
        android.media.ExifInterface.ORIENTATION_TRANSPOSE -> { mx.postRotate(90f); mx.postScale(-1f, 1f) }
        android.media.ExifInterface.ORIENTATION_TRANSVERSE -> { mx.postRotate(270f); mx.postScale(-1f, 1f) }
        else -> return b
    }
    return try {
        android.graphics.Bitmap.createBitmap(b, 0, 0, b.width, b.height, mx, true)
    } catch (_: OutOfMemoryError) { b } // a sideways photo beats a crash
}
