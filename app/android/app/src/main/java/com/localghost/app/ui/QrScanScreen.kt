package com.localghost.app.ui

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.localghost.app.net.EnrollLink
import com.localghost.app.qr.QrMatrixDecode
import com.localghost.app.qr.QrSampler
import com.localghost.app.ui.theme.*
import java.util.concurrent.Executors

/**
 * Camera QR scanner for enrolment. It previews the camera, runs each frame through the sampler +
 * matrix decoder, and on a successful decode of a localghost:// enrol link calls onLink. If the
 * camera permission is denied or scanning fails, the user falls back to the typed path , a bad scan
 * can never produce a wrong enrolment because the box fingerprint in the link must still match at
 * the TLS pin.
 */
@Composable
fun QrScanScreen(
    onLink: (EnrollLink) -> Unit,
    onCancel: () -> Unit,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    var granted by remember {
        mutableStateOf(
            ContextCompat.checkSelfPermission(context, Manifest.permission.CAMERA) ==
                PackageManager.PERMISSION_GRANTED
        )
    }
    var status by remember { mutableStateOf("point at the QR on the box") }
    // When a readable QR turns out not to be an enrol link, quip holds what it was + a dry line.
    // lastQuipFor debounces it so the same code does not re-fire every frame while it sits in view.
    var quip by remember { mutableStateOf<com.localghost.app.qr.QrGuess?>(null) }
    var lastQuipFor by remember { mutableStateOf<String?>(null) }
    // Geometry of the QR currently in view, for the AR overlay. Null when nothing is detected.
    var overlay by remember { mutableStateOf<Overlay?>(null) }
    // On a valid enrol scan we latch the link here and play a brief happy-ghost animation, then hand
    // off to onLink. Latching also stops the scanner re-triggering on later frames of the same code.
    var foundLink by remember { mutableStateOf<EnrollLink?>(null) }
    var enrolAnim by remember { mutableStateOf(0f) } // 0..1 over the animation
    // Two-rate sampling. HUNTING (no code in view) decodes at most every 500ms , cheap, the common
    // case is pointing at nothing. FOUND (a not-for-us code held in view) decodes every 100ms so the
    // overlay tracks the code and the sad ghost animates smoothly. lastDecodeAt gates the rate;
    // lastSeenAt lets found-mode time out when the code leaves the frame. Plain Longs in remember,
    // read/written only on the analysis thread, so no atomics needed.
    val timing = remember { ScanTiming() }

    val permLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { ok -> granted = ok }

    LaunchedEffect(Unit) {
        if (!granted) permLauncher.launch(Manifest.permission.CAMERA)
    }

    // On a found box: kick the enrol off in the BACKGROUND immediately (onLink starts the network
    // request in MainActivity), and overlap it with a ~1.5s animation , the QR becomes a box, a ghost
    // flies from it to the phone. The work and the delight happen at once, so the animation is not dead
    // waiting time. onLink is called once, at the start; the screen navigates away when MainActivity
    // flips state, and the animation is just what the person sees while the request is in flight.
    LaunchedEffect(foundLink) {
        val link = foundLink ?: return@LaunchedEffect
        onLink(link) // start enrolling now, in the background
        val steps = 30
        for (i in 1..steps) {
            enrolAnim = i.toFloat() / steps
            kotlinx.coroutines.delay(50) // 30 * 50ms = 1500ms
        }
    }

    Column(Modifier.fillMaxSize().padding(20.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        SectionLabel("SCAN THE BOX QR")
        Spacer(Modifier.height(12.dp))
        if (!granted) {
            Text("Camera permission is needed to scan. You can also go back and type the box " +
                 "address, code and fingerprint by hand.",
                 color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(16.dp))
            GhostButton("BACK TO TYPED ENTRY", onCancel, modifier = Modifier.fillMaxWidth())
            return@Column
        }

        val analysisExecutor = remember { Executors.newSingleThreadExecutor() }
        DisposableEffect(Unit) { onDispose { analysisExecutor.shutdown() } }

        // The PreviewView is created once and kept; the camera is bound to the lifecycle in a
        // LaunchedEffect below, NOT in the view factory. Binding in the factory ran once and never
        // rebound, so after the activity backgrounded (e.g. the camera permission dialog, which stops
        // the activity and tears the camera down) the camera never came back and the screen sat dead.
        // bindToLifecycle with the real lifecycleOwner handles stop/resume itself, so the camera
        // returns when the app does.
        val previewView = remember { PreviewView(context) }

        LaunchedEffect(granted) {
            if (!granted) return@LaunchedEffect
            val provider = withContext(Dispatchers.IO) {
                ProcessCameraProvider.getInstance(context).get()
            }
            val preview = androidx.camera.core.Preview.Builder().build().also {
                it.surfaceProvider = previewView.surfaceProvider
            }
            val analysis = ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .build()
            analysis.setAnalyzer(analysisExecutor) { proxy ->
                // Two-rate gate. In found mode (a quip is showing) decode every 100ms to track
                // the code and animate; otherwise hunt at 500ms. Skipped frames close cheaply.
                val now = System.currentTimeMillis()
                val interval = if (quip != null) 100L else 500L
                if (now - timing.lastDecodeAt < interval) {
                    proxy.close()
                    return@setAnalyzer
                }
                timing.lastDecodeAt = now
                val result = tryDecode(proxy)
                proxy.close()
                when (result) {
                    is ScanResult.Enrol -> {
                        // Found the box. Latch it and play a brief happy-ghost animation, then
                        // hand off. Latch once (ignore later frames of the same scan) so the
                        // animation is not restarted and onLink fires exactly once.
                        if (foundLink == null) {
                            status = "found ${result.link.host}"
                            overlay = result.overlay
                            quip = null
                            foundLink = result.link
                        }
                    }
                    is ScanResult.NotForUs -> {
                        // A readable code that is not the way in. Anchor the overlay and let the
                        // sad ghost orbit it. Refresh the overlay every pass so it tracks; debounce
                        // the quip text so it does not re-fire. Mark lastSeenAt for the timeout.
                        overlay = result.overlay
                        timing.lastSeenAt = now
                        if (result.guess.preview != lastQuipFor) {
                            lastQuipFor = result.guess.preview
                            quip = result.guess
                        }
                    }
                    ScanResult.Nothing -> {
                        // No readable QR this frame. In found mode, keep the ghost for a short
                        // grace period (the code may just have blurred for a frame); once it has
                        // been gone past the timeout, drop back to hunting and clear the ghost.
                        if (quip != null && now - timing.lastSeenAt > FOUND_TIMEOUT_MS) {
                            quip = null
                            lastQuipFor = null
                            overlay = null
                        } else if (quip == null) {
                            overlay = null
                        }
                    }
                }
            }
            provider.unbindAll()
            provider.bindToLifecycle(
                lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis
            )
        }

        Box(Modifier.weight(1f).fillMaxWidth()) {
            AndroidView(factory = { previewView }, modifier = Modifier.fillMaxSize())

            // AR overlay: a bracket on the code and a sad ghost orbiting it, because the code decoded
            // but it is not the way in. The orbit phase advances on its own clock (~100ms) so the ghost
            // flies even between decode passes. The finder points come from the analysis frame (image
            // space); we map them to this Canvas's view space. HONEST NOTE: the mapping is the part to
            // verify on a real device , camera resolution vs preview size vs rotation is the classic
            // source of an offset or mirrored overlay and cannot be confirmed by reasoning alone.
            var orbit by remember { mutableStateOf(0f) }
            LaunchedEffect(quip != null) {
                while (quip != null) {
                    orbit += 0.18f
                    kotlinx.coroutines.delay(100)
                }
            }
            overlay?.let { ov ->
                Canvas(Modifier.fillMaxSize()) {
                    val pts = mapFindersToView(ov, size.width, size.height)
                    if (pts.size == 3) {
                        val enrolling = foundLink != null
                        val color = if (enrolling) TerminalGreen else Warning
                        val minX = pts.minOf { it.x }; val maxX = pts.maxOf { it.x }
                        val minY = pts.minOf { it.y }; val maxY = pts.maxOf { it.y }
                        val cx = (minX + maxX) / 2f
                        val cy = (minY + maxY) / 2f
                        val pad = 24f
                        // bracket on the code (green when enrolling, amber for a wrong code)
                        drawRect(
                            color = color,
                            topLeft = androidx.compose.ui.geometry.Offset(minX - pad, minY - pad),
                            size = androidx.compose.ui.geometry.Size((maxX - minX) + pad * 2, (maxY - minY) + pad * 2),
                            style = androidx.compose.ui.graphics.drawscope.Stroke(width = 4f),
                        )
                        if (enrolling) {
                            // QR -> box -> ghost flies to the phone, over enrolAnim 0..1.
                            drawEnrolAnimation(cx, cy, (maxX - minX), (maxY - minY), size.height, enrolAnim, color)
                        } else if (quip != null) {
                            // a wrong code. The sad ghost orbits the bounding box.
                            val rx = (maxX - minX) / 2f + 80f
                            val ry = (maxY - minY) / 2f + 80f
                            val gx = cx + rx * kotlin.math.cos(orbit)
                            val gy = cy + ry * kotlin.math.sin(orbit)
                            drawSadGhost(gx, gy, 34f, color)
                        }
                    }
                }
            }
        }

        Spacer(Modifier.height(12.dp))
        Text("> $status", color = TerminalGreen, style = MaterialTheme.typography.labelMedium)

        // When a readable-but-wrong QR is in view, say what it was and have an opinion. This is the fix
        // for the old silence: a valid WiFi/URL/contact code now gets named, not swallowed.
        quip?.let { g ->
            Spacer(Modifier.height(10.dp))
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(VoidLighter, MaterialTheme.shapes.small)
                    .padding(12.dp)
            ) {
                Text("Looks like ${g.label}.", color = Warning, style = MaterialTheme.typography.bodyMedium)
                Spacer(Modifier.height(4.dp))
                Text(g.quip, color = TerminalGreen, style = MaterialTheme.typography.bodySmall)
                Spacer(Modifier.height(6.dp))
                Text(g.preview, color = GhostTextDim, style = MaterialTheme.typography.labelSmall)
            }
        }

        Spacer(Modifier.height(8.dp))
        GhostButton("CANCEL / TYPE INSTEAD", onCancel, modifier = Modifier.fillMaxWidth())
    }
}

/**
 * The three outcomes of looking at a frame. Nothing (no readable QR, stay quiet, the person is still
 * lining up the shot), Enrol (a real localghost enrol link, proceed), or NotForUs (a QR that decoded
 * fine but is not an enrol link, where we say what it is and have an opinion). The found cases carry
 * the QR's finder points and the frame geometry so the overlay can be anchored to the actual code.
 */
private sealed interface ScanResult {
    object Nothing : ScanResult
    data class Enrol(val link: EnrollLink, val overlay: Overlay) : ScanResult
    data class NotForUs(val guess: com.localghost.app.qr.QrGuess, val overlay: Overlay) : ScanResult
}

/**
 * Where the QR is, so the UI can draw on it. finders are the three finder-pattern centres in image
 * pixel space; frameW/frameH are the analysis frame size; rotation is the degrees the frame must be
 * rotated to be upright (from the ImageProxy). The Canvas maps these to view space.
 */
private data class Overlay(
    val finders: List<com.localghost.app.qr.QrSampler.FinderPoint>,
    val frameW: Int,
    val frameH: Int,
    val rotation: Int,
)

/**
 * Map the QR finder points from analysis-frame image space to Canvas view space. Two steps: rotate the
 * image-space point so it is upright (the back camera usually delivers frames rotated 90 degrees from
 * the portrait preview), then scale and centre for PreviewView's default FILL_CENTER crop (scale by the
 * larger ratio so the image fills the view, and offset by the cropped overflow).
 *
 * HONEST NOTE: this is the fiddly part and only a real device confirms it. The rotation cases other
 * than 90 are handled but untested, and front-camera mirroring is not (the scanner uses the back
 * camera). If the box lands offset or mirrored on a device, this function is where the fix goes.
 */
private fun mapFindersToView(
    ov: Overlay,
    viewW: Float,
    viewH: Float,
): List<androidx.compose.ui.geometry.Offset> {
    // upright frame dimensions after applying rotation
    val (upW, upH) = when (ov.rotation) {
        90, 270 -> ov.frameH.toFloat() to ov.frameW.toFloat()
        else -> ov.frameW.toFloat() to ov.frameH.toFloat()
    }
    // FILL_CENTER: scale so the upright image covers the view, crop the overflow
    val scale = maxOf(viewW / upW, viewH / upH)
    val dx = (viewW - upW * scale) / 2f
    val dy = (viewH - upH * scale) / 2f

    return ov.finders.map { p ->
        // rotate image-space (p.x, p.y) into upright space
        val (rx, ry) = when (ov.rotation) {
            90 -> (ov.frameH - p.y).toFloat() to p.x.toFloat()
            180 -> (ov.frameW - p.x).toFloat() to (ov.frameH - p.y).toFloat()
            270 -> p.y.toFloat() to (ov.frameW - p.x).toFloat()
            else -> p.x.toFloat() to p.y.toFloat()
        }
        androidx.compose.ui.geometry.Offset(rx * scale + dx, ry * scale + dy)
    }
}

/** Mutable sampling timestamps, held in remember and touched only on the analysis thread. */
private class ScanTiming {
    var lastDecodeAt = 0L
    var lastSeenAt = 0L
}

/** How long a found code can be missing before found-mode drops back to hunting. */
private const val FOUND_TIMEOUT_MS = 1500L

/** The ghost silhouette body (dome top, scalloped bottom), centred at (cx, cy), size s. */
private fun ghostBody(cx: Float, cy: Float, s: Float): androidx.compose.ui.graphics.Path =
    androidx.compose.ui.graphics.Path().apply {
        moveTo(cx - s, cy + s)
        cubicTo(cx - s, cy - s * 1.3f, cx + s, cy - s * 1.3f, cx + s, cy + s)
        val n = 3
        val step = (2 * s) / n
        var x = cx + s
        for (i in 0 until n) {
            val nx = x - step
            val midY = if (i % 2 == 0) cy + s * 1.35f else cy + s * 0.75f
            quadraticTo((x + nx) / 2f, midY, nx, cy + s)
            x = nx
        }
        close()
    }

/**
 * The brand ghost, sad variant, centred at (cx, cy). Downturned eye strokes so it reads as sad about
 * the wrong code. Pure Canvas, so it animates wherever the orbit puts it.
 */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawSadGhost(
    cx: Float, cy: Float, s: Float, color: androidx.compose.ui.graphics.Color,
) {
    drawPath(ghostBody(cx, cy, s), color, style = androidx.compose.ui.graphics.drawscope.Stroke(width = 3f))
    val eyeY = cy - s * 0.1f
    val eo = s * 0.42f
    val el = s * 0.28f
    drawLine(color,
        androidx.compose.ui.geometry.Offset(cx - eo - el / 2, eyeY - el / 2),
        androidx.compose.ui.geometry.Offset(cx - eo + el / 2, eyeY + el / 2), strokeWidth = 3f)
    drawLine(color,
        androidx.compose.ui.geometry.Offset(cx + eo - el / 2, eyeY + el / 2),
        androidx.compose.ui.geometry.Offset(cx + eo + el / 2, eyeY - el / 2), strokeWidth = 3f)
}

/**
 * The brand ghost, happy variant, centred at (cx, cy). Round eyes and an upturned smile, for the
 * moment a real box is found. Drawn rising out of the code on the enrol animation.
 */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawHappyGhost(
    cx: Float, cy: Float, s: Float, color: androidx.compose.ui.graphics.Color,
) {
    drawPath(ghostBody(cx, cy, s), color, style = androidx.compose.ui.graphics.drawscope.Stroke(width = 3f))
    val eyeY = cy - s * 0.1f
    val eo = s * 0.42f
    // round happy eyes
    drawCircle(color, radius = s * 0.13f, center = androidx.compose.ui.geometry.Offset(cx - eo, eyeY))
    drawCircle(color, radius = s * 0.13f, center = androidx.compose.ui.geometry.Offset(cx + eo, eyeY))
    // an upturned smile (a shallow arc), drawn as a small quad-curve path
    val smile = androidx.compose.ui.graphics.Path().apply {
        moveTo(cx - s * 0.4f, cy + s * 0.35f)
        quadraticTo(cx, cy + s * 0.75f, cx + s * 0.4f, cy + s * 0.35f)
    }
    drawPath(smile, color, style = androidx.compose.ui.graphics.drawscope.Stroke(width = 3f))
}

/**
 * The found-the-box animation, over t in 0..1, centred at the code (cx, cy) with the code's size
 * (w, h). Three phases: the code becomes a small box (a drawn cube), a happy ghost emerges from it,
 * then the ghost flies down toward the phone (the bottom of the screen, viewH) and fades. Pure Canvas.
 */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawEnrolAnimation(
    cx: Float, cy: Float, w: Float, h: Float, viewH: Float, t: Float, color: androidx.compose.ui.graphics.Color,
) {
    val stroke = androidx.compose.ui.graphics.drawscope.Stroke(width = 4f)
    // phase 1 (0..0.35): the code condenses into a small isometric box
    // phase 2 (0.35..0.65): a ghost emerges from the top of the box
    // phase 3 (0.65..1): the ghost flies down to the phone and fades
    val boxSize = (minOf(w, h) * 0.6f).coerceAtLeast(60f)

    // the box (drawn the whole time, fading slightly as the ghost leaves)
    val boxAlpha = (1f - ((t - 0.65f) / 0.35f)).coerceIn(0.25f, 1f)
    drawIsoBox(cx, cy, boxSize, color.copy(alpha = boxAlpha), stroke)

    // the ghost: position interpolates from the box (phase 2) down toward the phone (phase 3)
    if (t >= 0.35f) {
        val gp = ((t - 0.35f) / 0.65f).coerceIn(0f, 1f) // 0 at emerge, 1 at phone
        val startY = cy - boxSize * 0.2f
        val endY = viewH - 80f
        val gy = startY + (endY - startY) * gp
        val ghostAlpha = (1f - gp).coerceIn(0f, 1f) * 0.9f + 0.1f
        drawHappyGhost(cx, gy, 32f * (1f - gp * 0.4f), color.copy(alpha = ghostAlpha))
    }
}

/** A simple isometric box outline centred at (cx, cy), edge e. The QR "becomes" this. */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawIsoBox(
    cx: Float, cy: Float, e: Float, color: androidx.compose.ui.graphics.Color,
    stroke: androidx.compose.ui.graphics.drawscope.Stroke,
) {
    val half = e / 2f
    val d = e * 0.4f // depth offset for the iso effect
    fun o(x: Float, y: Float) = androidx.compose.ui.geometry.Offset(cx + x, cy + y)
    // front face
    val fTL = o(-half, -half + d * 0.5f); val fTR = o(half, -half + d * 0.5f)
    val fBL = o(-half, half + d * 0.5f); val fBR = o(half, half + d * 0.5f)
    // top-back edge
    val bTL = o(-half + d, -half - d * 0.5f); val bTR = o(half + d, -half - d * 0.5f)
    drawLine(color, fTL, fTR, strokeWidth = stroke.width)
    drawLine(color, fTR, fBR, strokeWidth = stroke.width)
    drawLine(color, fBR, fBL, strokeWidth = stroke.width)
    drawLine(color, fBL, fTL, strokeWidth = stroke.width)
    drawLine(color, fTL, bTL, strokeWidth = stroke.width)
    drawLine(color, fTR, bTR, strokeWidth = stroke.width)
    drawLine(color, bTL, bTR, strokeWidth = stroke.width)
}

/** Pull luminance from the frame, sample a grid, decode, and classify. */
private fun tryDecode(proxy: ImageProxy): ScanResult {
    return try {
        val plane = proxy.planes[0]
        val buffer = plane.buffer
        val rowStride = plane.rowStride
        val w = proxy.width
        val h = proxy.height
        val lum = IntArray(w * h)
        val data = ByteArray(buffer.remaining())
        buffer.get(data)
        for (y in 0 until h) {
            val base = y * rowStride
            for (x in 0 until w) lum[y * w + x] = data[base + x].toInt() and 0xFF
        }
        // Diagnostic sample: reports which stage the pipeline reaches (binarise, finders, grid) so the
        // on-screen debug line can show why a real camera frame is not decoding.
        val (sampled, diag) = QrSampler.sampleDiag(lum, w, h)
        ScanDiag.last = diag.note
        if (sampled == null) return ScanResult.Nothing
        val overlay = Overlay(sampled.finders, w, h, proxy.imageInfo.rotationDegrees)
        val text = try {
            QrMatrixDecode.decode(sampled.grid)
        } catch (e: Exception) {
            // grid found but did not decode (logo in the middle, glare, perspective). Report it.
            ScanDiag.last = "grid ${sampled.grid.size} found, decode failed: ${e.message ?: "error"}"
            return ScanResult.Nothing
        }
        ScanDiag.last = "decoded ${text.length} chars"
        when (val r = EnrollLink.parseResult(text)) {
            is EnrollLink.Result.Ok -> ScanResult.Enrol(r.link, overlay)
            is EnrollLink.Result.Outdated -> ScanResult.NotForUs(
                com.localghost.app.qr.QrGuess(
                    "a newer box",
                    "That code is from a newer LocalGhost than this app. Update the app and try again.",
                    "enrol v${r.sawVersion}",
                ),
                overlay,
            )
            EnrollLink.Result.Malformed -> ScanResult.NotForUs(
                com.localghost.app.qr.QrContent.classify(text), overlay,
            )
            EnrollLink.Result.NotEnroll -> ScanResult.NotForUs(
                com.localghost.app.qr.QrContent.classify(text), overlay,
            )
        }
    } catch (t: Throwable) {
        // Decode failed. Catches Throwable, not just Exception, ON PURPOSE: a poisoned QR is
        // adversarial input to a hand-rolled decoder, and the failure modes include Errors (an
        // OutOfMemoryError from a pathological allocation, a StackOverflowError from deep recursion),
        // not only Exceptions. A scanned code must never be able to crash the app, so any throwable
        // here becomes "no readable QR" and the person keeps scanning or types the values. A bad scan
        // can never enrol anyway, because the fingerprint pin must still match.
        ScanDiag.last = "frame error: ${t.message ?: t.javaClass.simpleName}"
        ScanResult.Nothing
    }
}

/** Thread-safe holder for the latest scan-pipeline diagnostic, read by the status line. */
private object ScanDiag {
    @Volatile var last: String = "starting"
}
