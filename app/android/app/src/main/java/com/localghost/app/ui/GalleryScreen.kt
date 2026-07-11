package com.localghost.app.ui

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import com.localghost.app.net.BoxClient
import com.localghost.app.ui.theme.*

/**
 * GALLERY , a grid of what the BOX has archived (not the phone's own roll: this is the proof your
 * copies exist). Newest first, paged 60 at a time, thumbnails streamed from /v1/frames/thumb (webp
 * when the box has cwebp installed, jpeg otherwise). Videos show a play glyph until video
 * thumbnailing (a frame grab) exists box-side.
 */
@Composable
fun GalleryScreen() {
    val ctx = LocalContext.current
    var frames by remember { mutableStateOf(listOf<BoxClient.GalleryFrame>()) }
    var loading by remember { mutableStateOf(true) }
    var endReached by remember { mutableStateOf(false) }
    val thumbs = remember { mutableStateMapOf<String, android.graphics.Bitmap?>() }

    suspend fun loadPage() {
        loading = true
        val before = frames.lastOrNull()?.takenAt ?: 0L
        val page = BoxClient.framesList(ctx, before)
        if (page.isEmpty()) endReached = true else frames = frames + page
        loading = false
    }

    LaunchedEffect(Unit) { loadPage() }

    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp)) {
        Spacer(Modifier.height(16.dp))
        Text("> GALLERY", color = TerminalGreen, style = MaterialTheme.typography.titleMedium)
        Spacer(Modifier.height(4.dp))
        Text(
            "What is archived on your box , copies streamed from the encrypted volume, newest first.",
            color = TerminalDim, style = MaterialTheme.typography.bodySmall)
        Spacer(Modifier.height(12.dp))

        if (frames.isEmpty() && !loading) {
            Text("! nothing archived yet , run a sync", color = TerminalDim,
                style = MaterialTheme.typography.bodyMedium)
        }

        LazyVerticalGrid(
            columns = GridCells.Fixed(3),
            horizontalArrangement = Arrangement.spacedBy(4.dp),
            verticalArrangement = Arrangement.spacedBy(4.dp),
            modifier = Modifier.weight(1f)
        ) {
            items(frames, key = { it.hash }) { frame ->
                // Fetch each thumb once; null means tried-and-missing (video or fetch failure).
                LaunchedEffect(frame.hash) {
                    if (!thumbs.containsKey(frame.hash)) {
                        // Cap the in-memory cache , a multi-thousand-photo archive would otherwise
                        // accumulate every decoded bitmap and OOM. Evicting arbitrary old entries is
                        // fine: scrolled-back-to cells just refetch (thumbs are small and local).
                        if (thumbs.size > 240) {
                            thumbs.keys.take(60).forEach { thumbs.remove(it) }
                        }
                        val bytes = BoxClient.frameThumb(ctx, frame.hash)
                        thumbs[frame.hash] =
                            bytes?.let { BitmapFactory.decodeByteArray(it, 0, it.size) }
                    }
                }
                Box(
                    Modifier.aspectRatio(1f).background(VoidLighter),
                    contentAlignment = Alignment.Center
                ) {
                    val bmp = thumbs[frame.hash]
                    when {
                        bmp != null -> Image(
                            bitmap = bmp.asImageBitmap(), contentDescription = null,
                            contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
                        frame.kind == "video" -> Text("▶", color = TerminalGreen,
                            style = MaterialTheme.typography.titleLarge)
                        else -> Text("·", color = TerminalDim)
                    }
                }
            }
            if (!endReached) {
                item(span = { androidx.compose.foundation.lazy.grid.GridItemSpan(3) }) {
                    var wantMore by remember { mutableStateOf(false) }
                    LaunchedEffect(wantMore) { if (wantMore) { loadPage(); wantMore = false } }
                    Box(Modifier.fillMaxWidth().padding(vertical = 12.dp),
                        contentAlignment = Alignment.Center) {
                        GhostButton(if (loading) "LOADING…" else "LOAD MORE",
                            onClick = { if (!loading) wantMore = true }, enabled = !loading)
                    }
                }
            }
        }
    }
}
