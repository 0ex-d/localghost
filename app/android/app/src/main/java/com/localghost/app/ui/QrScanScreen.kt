package com.localghost.app.ui

import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.FocusMeteringAction
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
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
import androidx.compose.ui.graphics.nativeCanvas
import java.util.concurrent.Executors

/**
 * Camera QR scanner for enrolment. It previews the camera, runs each frame through the sampler +
 * matrix decoder, and on a successful decode of a localghost:// enrol link calls onScanned. If the
 * camera permission is denied or scanning fails, the user falls back to the typed path , a bad scan
 * can never produce a wrong enrolment because the box fingerprint in the link must still match at
 * the TLS pin.
 */
@Composable
fun QrScanScreen(
    onScanned: (EnrollLink) -> Unit,
    onProceed: () -> Unit,
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
    // Detection tick , flashes on EVERY successful decode, repeats included. "I saw it" and
    // "I did something about it" are different facts; the scanner now reports both.
    var tickAt by remember { mutableStateOf(0L) }
    var tickShow by remember { mutableStateOf(false) }
    LaunchedEffect(tickAt) {
        if (tickAt > 0L) {
            tickShow = true
            kotlinx.coroutines.delay(650)
            tickShow = false
        }
    }
    // Live decode diagnostic, surfaced on screen. ScanDiag.last is written by the analyser thread each
    // frame with the exact stage reached (finder count, grid size, "no candidate decoded", "decoded N
    // chars"), so polling it here shows WHERE a code is getting stuck instead of failing silently.
    var diag by remember { mutableStateOf("") }
    var coach by remember { mutableStateOf<String?>(null) } // "move closer" / "hold steady" hint, or null
    LaunchedEffect(Unit) {
        while (true) {
            diag = ScanDiag.last
            // Coaching from the shared streak counter: only after a sustained run of finders-but-no-decode
            // (~1.5s), turn module pixel size into a "move closer / back / focus" hint. Cleared as soon as
            // anything decodes (tryDecode zeroes the streak).
            val streak = com.localghost.app.qr.QrSampler.ScanGeom.noDecodeStreak
            coach = if (streak >= 6) {
                val mod = com.localghost.app.qr.QrSampler.ScanGeom.moduleLenPx
                when {
                    mod in 0.1..3.0 -> "move closer , the code is too small to read"
                    mod > 9.0 -> "move back a little , the code is too big for the frame"
                    else -> "hold steady and tap the code to focus"
                }
            } else null
            kotlinx.coroutines.delay(180)
        }
    }
    // When a readable QR turns out not to be an enrol link, quip holds what it was + a dry line.
    // lastQuipFor debounces it so the same code does not re-fire every frame while it sits in view.
    var quip by remember { mutableStateOf<com.localghost.app.qr.QrGuess?>(null) }
    var lastQuipFor by remember { mutableStateOf<String?>(null) }
    // Geometry of the QR currently in view, for the AR overlay. Null when nothing is detected.
    var overlay by remember { mutableStateOf<Overlay?>(null) }
    // On a valid enrol scan we latch the link here and play the happy-ghost animation. onScanned fires
    // at once to begin enrolling; onProceed fires when the animation ends. Latching also stops the
    // scanner re-triggering on later frames of the same code.
    var foundLink by remember { mutableStateOf<EnrollLink?>(null) }
    val haptic = androidx.compose.ui.platform.LocalHapticFeedback.current
    // Accumulates multi-frame enrolment QRs across camera frames. Remembered so it survives recompositions.
    val frames = remember { com.localghost.app.qr.FrameAssembler() }
    // The erasure-coded set (LGQR2) a current box rotates: any K of its K+M frames complete it.
    val stream = remember { com.localghost.app.qr.StreamAssembler() }
    var frameProgress by remember { mutableStateOf<Pair<Int, Int>?>(null) }
    var capturedFrames by remember { mutableStateOf<Set<Int>>(emptySet()) }
    var frameFlashAt by remember { mutableStateOf(0L) } // timestamp of the last new-frame pulse
    var enrolAnim by remember { mutableStateOf(0f) } // 0..1 over the animation
    // Two-frame confirmation gate. A live scanner runs many decode attempts per frame (8 orientations,
    // several versions and biases); very occasionally one lands a Reed-Solomon miscorrection that is
    // internally consistent but wrong. A wrong decode is worse than no decode here, so we never act on a
    // payload the first time we see it , we require the SAME payload on two decodes before promoting it.
    // A real code repeats frame to frame; a random miscorrection does not. pendingPayload holds the
    // last frame's payload awaiting a match; it is cleared whenever the chain breaks.
    var pendingPayload by remember { mutableStateOf<String?>(null) }
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

    // On a valid enrol scan: kick the enrol off in the BACKGROUND immediately (onScanned starts the
    // network request in the host), then play a fixed ~2.6s AR success animation over the live camera ,
    // the aperture blows open like the unlock's iris, BOX FOUND snaps in and the address and fingerprint
    // type themselves out. The enrol and the animation run at once, so the time is not dead waiting even
    // though the box can take a moment to answer. Only once it has fully played do we call onProceed to
    // leave the scanner, so the success is always seen for its full length; the host routes on the outcome.
    LaunchedEffect(foundLink) {
        val link = foundLink ?: return@LaunchedEffect
        onScanned(link) // start enrolling now, in the background
        val steps = 52
        for (i in 1..steps) {
            enrolAnim = i.toFloat() / steps
            kotlinx.coroutines.delay(50) // 52 * 50ms = 2600ms
        }
        onProceed() // the open has played , hand off
    }

    // A free clock while a real box is found, for the held ring's faint breathe.
    var celebrate by remember { mutableStateOf(0f) }
    LaunchedEffect(foundLink) {
        while (foundLink != null) {
            celebrate += 0.06f
            kotlinx.coroutines.delay(40)
        }
        celebrate = 0f
    }

    Box(Modifier.fillMaxSize().background(Void)) {
        if (!granted) {
            Column(
                Modifier.fillMaxSize().systemBarsPadding().padding(20.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                SectionLabel("SCAN THE BOX QR")
                Spacer(Modifier.height(12.dp))
                Text("Camera permission is needed to scan. You can also go back and type the box " +
                     "address, code and fingerprint by hand.",
                     color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
                Spacer(Modifier.height(16.dp))
                GhostButton("BACK TO TYPED ENTRY", onCancel, modifier = Modifier.fillMaxWidth())
            }
            return@Box
        }

        val analysisExecutor = remember { Executors.newSingleThreadExecutor() }

        // The PreviewView is created once and kept; the camera is bound to the lifecycle in a
        // LaunchedEffect below, NOT in the view factory. Binding in the factory ran once and never
        // rebound, so after the activity backgrounded (e.g. the camera permission dialog, which stops
        // the activity and tears the camera down) the camera never came back and the screen sat dead.
        // bindToLifecycle with the real lifecycleOwner handles stop/resume itself, so the camera
        // returns when the app does.
        val previewView = remember { PreviewView(context) }
        // The bound Camera, kept so the tap-to-focus gesture below can drive its CameraControl.
        var camera by remember { mutableStateOf<androidx.camera.core.Camera?>(null) }
        // AUTO-TORCH , after five rounds the detector is algorithm-complete; what defeats it now
        // is a dim hallway, same as every scanner. Mean luma below the floor for ~15 frames
        // lights the torch; comfortably bright for as long puts it out. Hysteresis, not a
        // flicker; the person can always cover the lens if they disagree.
        var darkFrames by remember { mutableStateOf(0) }
        var brightFrames by remember { mutableStateOf(0) }
        var torchOn by remember { mutableStateOf(false) }
        // AUTO-ZOOM , a code that keeps showing finders but never decodes at under ~5 px per
        // module is pixel-starved, not misread: on a 720p analysis frame a v8 symbol filling half
        // the short axis is ~6 px per module, and nearest-pixel sampling inside the module has
        // two or three distinct pixels to vote with. The phone's own zoom is real detail (720p is
        // a downscale of the sensor), so after a sustained no-decode streak on a small code, zoom
        // 2x; back out when the code grows past what the frame holds comfortably.
        // The two thresholds must not meet across the 2x: zooming in doubles px/module, so a code at
        // 4.9 px/module became 9.8, which was past the old 9.5 back-out line, which put it back at
        // 4.9, which zoomed it in again , the scanner breathed in and out every half second. Now
        // the back-out line is 14 (a code that was 7 unzoomed, comfortably readable) and no zoom
        // change follows another within two seconds; back out also when no code has been seen at
        // all for a while (the person moved on), so the next code starts wide.
        var zoomed by remember { mutableStateOf(false) }
        var zoomChangedAt by remember { mutableLongStateOf(0L) }
        LaunchedEffect(Unit) {
            while (true) {
                val cam = camera
                val streak = com.localghost.app.qr.QrSampler.ScanGeom.noDecodeStreak
                val mod = com.localghost.app.qr.QrSampler.ScanGeom.moduleLenPx
                val now = System.currentTimeMillis()
                if (cam != null && now - zoomChangedAt > 2000) {
                    val maxZoom = cam.cameraInfo.zoomState.value?.maxZoomRatio ?: 1f
                    val codeSeen = now - timing.lastDetectAt < DETECT_WINDOW_MS
                    if (!zoomed && codeSeen && streak >= 6 && mod in 0.1..5.0 && maxZoom >= 1.9f) {
                        runCatching { cam.cameraControl.setZoomRatio(2f) }
                        zoomed = true; zoomChangedAt = now
                        ScanDiag.last = "zoomed 2x (${"%.1f".format(mod)} px/module)"
                    } else if (zoomed && ((codeSeen && mod > 14.0) || now - timing.lastDetectAt > 4000)) {
                        runCatching { cam.cameraControl.setZoomRatio(1f) }
                        zoomed = false; zoomChangedAt = now
                    }
                }
                kotlinx.coroutines.delay(250)
            }
        }
        // Leaving the screen (a decode, back, the app going away): the torch OFF and the camera
        // unbound, explicitly. bindToLifecycle follows the ACTIVITY's lifecycle, which stays alive
        // when this composable leaves, so without this the torch stayed lit and the camera stayed
        // open until the app was backgrounded.
        DisposableEffect(Unit) {
            onDispose {
                runCatching {
                    camera?.cameraControl?.enableTorch(false)
                    camera?.cameraControl?.setZoomRatio(1f)
                    ProcessCameraProvider.getInstance(context).get().unbindAll()
                }
                analysisExecutor.shutdown()
            }
        }
        // Tap-to-focus feedback ring: where the last tap landed and its fade clock. tapTick (not the
        // offset) keys the animation so tapping the same spot twice still replays the ring.
        var focusRingAt by remember { mutableStateOf<androidx.compose.ui.geometry.Offset?>(null) }
        var focusRingTick by remember { mutableIntStateOf(0) }
        var focusRingT by remember { mutableStateOf(1f) }
        LaunchedEffect(focusRingTick) {
            if (focusRingAt == null) return@LaunchedEffect
            focusRingT = 0f
            while (focusRingT < 1f) {
                kotlinx.coroutines.delay(30)
                focusRingT += 0.07f
            }
            focusRingAt = null
        }

        LaunchedEffect(granted) {
            if (!granted) return@LaunchedEffect
            val provider = withContext(Dispatchers.IO) {
                ProcessCameraProvider.getInstance(context).get()
            }
            val preview = androidx.camera.core.Preview.Builder().build().also {
                it.surfaceProvider = previewView.surfaceProvider
            }
            // Analysis resolution. A dense code (v9 enrol code is 57 modules, a v11 is 61) needs enough
            // pixels per module for the binariser and finder detection to resolve small modules. 720p gave
            // roughly 7px per module on a code filling the frame, which is why the big ones only decoded in
            // a narrow zoom band. 1080p gives ~50% more linear resolution, widening the workable distance.
            // Each frame is ~2x heavier to binarise and scan, but the frame throttle keeps the rate low, so
            // the extra cost is paid a few times a second, not thirty. If it runs warm, this is the dial.
            // ResolutionStrategy picks the closest the device actually supports.
            val resolutionSelector = androidx.camera.core.resolutionselector.ResolutionSelector.Builder()
                .setResolutionStrategy(
                    androidx.camera.core.resolutionselector.ResolutionStrategy(
                        // 720p, not 1080p , detection cost scales with pixels and the sampler
                        // binarises up to twice a frame (sticky + probe); 2.25x less work per
                        // pass means 2.25x more attempts per second, and the small-module
                        // leniency already covers what the resolution gives up at range.
                        android.util.Size(1280, 720),
                        androidx.camera.core.resolutionselector.ResolutionStrategy.FALLBACK_RULE_CLOSEST_HIGHER_THEN_LOWER,
                    )
                )
                .build()
            val analysis = ImageAnalysis.Builder()
                .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                .setResolutionSelector(resolutionSelector)
                .build()
            analysis.setAnalyzer(analysisExecutor) { proxy ->
                run {
                    // Subsampled mean luma straight off the Y plane , 1 pixel in 64, pennies.
                    val plane = proxy.planes[0]
                    val buf = plane.buffer.duplicate()
                    var sum = 0L; var n = 0
                    var i = 0
                    val lim = buf.limit()
                    while (i < lim) { sum += buf.get(i).toInt() and 0xFF; n++; i += 64 }
                    val mean = if (n > 0) (sum / n).toInt() else 128
                    if (mean < 55) { darkFrames++; brightFrames = 0 } else if (mean > 80) { brightFrames++; darkFrames = 0 }
                    if (!torchOn && darkFrames > 15) {
                        torchOn = true
                        camera?.cameraControl?.enableTorch(true)
                    } else if (torchOn && brightFrames > 15) {
                        torchOn = false
                        camera?.cameraControl?.enableTorch(false)
                    }
                }
                // Two-rate gate. The full pipeline (binarise, finder scan, multi-triple sample, decode) is
                // heavy, and running it flat out heats the phone and then thermally throttles, which is what
                // makes it feel slower over time. So we push HARD only when it matters: once a code has been
                // DETECTED in the recent past (finders found this frame, whether or not it decoded), sample
                // every ~180ms so a stubborn dense code gets many attempts a second and locks fast; when
                // nothing is in view, fall back to ~400ms hunting. The ghost and reticle animate on their own
                // clocks between decodes, so the rate itself is not felt.
                val now = System.currentTimeMillis()
                val codeInView = now - timing.lastDetectAt < DETECT_WINDOW_MS
                // Field-tuned down from 180/400: flat-out sampling heats the phone into thermal
                // throttle, which FEELS like the scanner getting worse the longer you try. 600ms
                // hunting is plenty to notice a code entering view within a blink.
                //
                // A code IN VIEW gets 150ms: the box now holds each rotating frame for ONE second
                // (twelve frames, any eight enough), so the first frame , the one that starts the
                // assembly burst , has about six attempts inside its window instead of the four
                // that 250ms left it. A code in view is the enrolment scan or a stray code held up
                // on purpose, a bounded moment either way, not the hunt the thermal tuning is for.
                //
                // During multi-frame ASSEMBLY, 100ms: capturing the rotating sequence is a burst
                // measured in seconds, ~10 attempts per one-second frame, and with erasure coding a
                // frame that still fails costs one more frame, not a lap. The burst ends when
                // assembly does, so nothing here can cook the phone.
                val assembling = capturedFrames.isNotEmpty() &&
                    frameProgress?.let { it.first < it.second } == true
                val interval = when {
                    assembling -> 100L
                    codeInView -> 150L
                    else -> 600L
                }
                if (now - timing.lastDecodeAt < interval) {
                    proxy.close()
                    return@setAnalyzer
                }
                timing.lastDecodeAt = now
                if (foundLink != null) {
                    // Enrolment already latched. The QR keeps rotating on the box, but there is nothing
                    // left to read , stop decoding entirely so duplicate frames cannot re-enter the parse
                    // path (which was causing the post-completion errors). The found overlay stays up.
                    proxy.close()
                    return@setAnalyzer
                }
                val result = tryDecode(proxy, frames, stream)
                proxy.close()
                // A code is "in view" when this frame either sampled a grid (corners set) or saw at least
                // two finder patterns , the marginal codes that fail to sample are exactly the ones that
                // need more attempts per second, and previously they never opened the fast window at all.
                // Two finders, not one: a single 1:1:3:1:1 coincidence in texture is common, two together
                // almost always means a real code, so the fast rate doesn't burn battery on wallpaper.
                if (com.localghost.app.qr.QrSampler.ScanGeom.corners != null ||
                    com.localghost.app.qr.QrSampler.ScanGeom.findersSeen >= 2) timing.lastDetectAt = now
                when (result) {
                    is ScanResult.Enrol -> {
                        tickAt = System.currentTimeMillis()
                        // Found the box. A CLEAN decode (plain RS, no erasures) that parsed as a valid enrol
                        // link is trustworthy on the first frame: the strict localghost:// pattern plus a
                        // well-formed 64-hex pinned fingerprint make a random miscorrection into a valid link
                        // effectively impossible, and a wrong fingerprint would fail the TLS pin anyway (fails
                        // safe , connection refused, re-scan). So we latch it at once. An erasure-path decode
                        // ("conf"/"logo", e.g. a logo or blurred code) is more willing to manufacture a
                        // consistent-but-wrong payload, so those still require the same payload on two frames.
                        val payload = "${result.link.host}:${result.link.port}:${result.link.code}:${result.link.certFingerprint}"
                        overlay = result.overlay
                        timing.lastSeenAt = now
                        if (foundLink == null) {
                            if (result.clean || pendingPayload == payload) {
                                status = "found ${result.link.host}"
                                quip = null
                                coach = null
                                foundLink = result.link
                            } else {
                                status = "reading…"
                                pendingPayload = payload
                            }
                        }
                    }
                    is ScanResult.NotForUs -> {
                        tickAt = System.currentTimeMillis()
                        // A readable code that is not the way in. Anchor the overlay and let the ghost
                        // orbit it. Only name it once the same payload has been seen twice, so a transient
                        // wrong decode never flashes the wrong opinion. Mark lastSeenAt for the timeout.
                        overlay = result.overlay
                        timing.lastSeenAt = now
                        val payload = result.guess.preview
                        if (pendingPayload == payload) {
                            if (result.guess.preview != lastQuipFor) {
                                lastQuipFor = result.guess.preview
                                quip = result.guess
                            }
                        } else {
                            pendingPayload = payload
                        }
                    }
                    is ScanResult.Frames -> {
                        tickAt = System.currentTimeMillis()
                        coach = null
                        // Mid-capture of a multi-frame identity. Anchor the overlay, show progress, and on
                        // a NEWLY captured frame fire a brief success pulse + haptic so each scan feels
                        // acknowledged. Already-scanned frames just keep their checkmark, no re-pulse.
                        overlay = result.overlay
                        timing.lastSeenAt = now
                        frameProgress = result.have to result.want
                        capturedFrames = result.captured
                        if (result.justCaptured) {
                            frameFlashAt = now
                            haptic.performHapticFeedback(HapticFeedbackType.LongPress)
                            status = "captured ${result.have} of ${result.want}"
                        } else {
                            status = "hold steady , ${result.have} of ${result.want}"
                        }
                    }
                    ScanResult.Nothing -> {
                        // No readable QR this frame. A gap breaks the confirmation chain. In found mode,
                        // keep the ghost for a short grace period (the code may just have blurred for a
                        // frame); once gone past the timeout, drop back to hunting and clear the ghost.
                        pendingPayload = null
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
            // Bind with a short retry. On a COLD first launch the camera device can still be held by
            // the OS (or the just-granted permission has not fully propagated), and bindToLifecycle
            // throws , which, uncaught, left a dead preview that only worked when you reopened the
            // scanner (second time the camera is free and permission is already settled). This is that
            // "open it twice" bug. A few spaced retries make the first open succeed.
            var bound = false
            var lastErr: Exception? = null
            repeat(5) { attempt ->
                if (bound) return@repeat
                try {
                    provider.unbindAll()
                    camera = provider.bindToLifecycle(
                        lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis
                    )
                    bound = true
                } catch (e: Exception) {
                    lastErr = e
                    kotlinx.coroutines.delay(250L * (attempt + 1)) // 250, 500, 750, 1000ms backoff
                }
            }
            if (!bound) {
                status = "camera busy , tap to retry"
                ScanDiag.last = "camera bind failed: ${lastErr?.javaClass?.simpleName}"
            }
        }

        // Full-bleed camera preview. The AR overlays and the text panels float over it. Tapping focuses
        // AND meters at that point: focus fixes close-range blur (a phone screen at 12cm sits at the edge
        // of the lens's comfort zone and hunts), and exposure metering on the tapped spot is the real win
        // for scanning a SCREEN , auto-exposure averages the dark room and blows the bright screen out,
        // crushing exactly the low-contrast grey marks (Samsung's ring finders) that detection needs.
        // previewView.meteringPointFactory maps view coordinates through the preview's own transform, so
        // the tap lands on the right sensor region regardless of crop or rotation. The action auto-cancels
        // back to continuous auto after a few seconds, so a stray tap can never leave the camera stuck.
        AndroidView(
            factory = { previewView },
            modifier = Modifier.fillMaxSize().pointerInput(Unit) {
                detectTapGestures { tap ->
                    val cam = camera ?: return@detectTapGestures
                    val point = previewView.meteringPointFactory.createPoint(tap.x, tap.y)
                    val action = FocusMeteringAction
                        .Builder(point, FocusMeteringAction.FLAG_AF or FocusMeteringAction.FLAG_AE)
                        .setAutoCancelDuration(6, java.util.concurrent.TimeUnit.SECONDS)
                        .build()
                    cam.cameraControl.startFocusAndMetering(action)
                    focusRingAt = tap
                    focusRingTick++
                }
            },
        )

        // Brief expanding ring where the tap landed, so focusing visibly registered.
        run {
            val at = focusRingAt
            if (at != null && focusRingT < 1f) {
                Canvas(Modifier.fillMaxSize()) {
                    drawCircle(
                        color = TerminalGreen.copy(alpha = (1f - focusRingT) * 0.85f),
                        radius = 40f * (1f + 0.5f * focusRingT),
                        center = at,
                        style = androidx.compose.ui.graphics.drawscope.Stroke(width = 3f),
                    )
                }
            }
        }

            // FEEDBACK RETICLE: whenever the detector has locked onto a code's position this frame
            // (ScanGeom.corners is set), draw a clean pulsing corner-bracket square around it, so the
            // person can see "yes, I'm seeing a code, hold steady" even while the decode is still being
            // worked out. It tracks the detected quad; it is feedback, not the decode verdict (that is
            // the ghost). Refreshed on its own ~80ms tick so it animates between decode passes.
            var geomTick by remember { mutableStateOf(0) }
            LaunchedEffect(granted) {
                while (granted) { geomTick++; kotlinx.coroutines.delay(80) }
            }
            // Reticle stabiliser. Raw per-frame corners are honest but twitchy: a one-frame finder
            // coincidence teleports the bracket across the screen, and even a solid lock breathes a
            // few pixels frame to frame. Three rules make it feel locked-on instead:
            //   REJECT , a quad whose corners jump more than a third of the frame within 400ms is a
            //            misdetection; keep showing the last good one.
            //   SMOOTH , accepted quads blend 40% toward the new position (EMA), absorbing breath.
            //   HOLD   , when detection drops, the last quad lingers 350ms so a missed frame or two
            //            does not blink the bracket while the person is holding perfectly still.
            val smooth = remember { object {
                var quad: List<com.localghost.app.qr.QrSampler.FinderPoint>? = null
                var at = 0L
            } }
            run {
                geomTick // read so this recomposes on the tick
                val corners = com.localghost.app.qr.QrSampler.ScanGeom.corners
                val fw = com.localghost.app.qr.QrSampler.ScanGeom.frameW
                val fh = com.localghost.app.qr.QrSampler.ScanGeom.frameH
                val rot = com.localghost.app.qr.QrSampler.ScanGeom.rotation
                val nowMs = System.currentTimeMillis()
                if (foundLink == null && corners != null && corners.size == 4 && quadLooksSquare(corners)) {
                    val prev = smooth.quad
                    val limit = (minOf(fw, fh) / 3f)
                    val jumped = prev != null && nowMs - smooth.at < 400 && prev.zip(corners).any { (a, b) ->
                        val dx = (a.x - b.x).toFloat(); val dy = (a.y - b.y).toFloat()
                        dx * dx + dy * dy > limit * limit
                    }
                    if (!jumped) {
                        smooth.quad = if (prev == null || prev.size != 4) corners
                        else prev.zip(corners).map { (a, b) ->
                            com.localghost.app.qr.QrSampler.FinderPoint(
                                a.x + ((b.x - a.x) * 0.4f).toInt(),
                                a.y + ((b.y - a.y) * 0.4f).toInt(),
                            )
                        }
                        smooth.at = nowMs
                    }
                } else if (nowMs - smooth.at > 350) {
                    smooth.quad = null
                }
                val q4 = smooth.quad
                if (foundLink == null && q4 != null) {
                    Canvas(Modifier.fillMaxSize()) {
                        val q = mapPointsToView(q4, fw, fh, rot, size.width, size.height)
                        val pulse = 0.5f + 0.5f * kotlin.math.sin(geomTick * 0.25f)
                        drawReticle(q, TerminalGreen, pulse)
                    }
                }
            }

            // AR overlay: the VAULT APERTURE over the code, in the unlock's language. It spins on its
            // own clock (~60ms) so it turns between decode passes; its segments light as the box's
            // rotating enrolment frames land, and it goes red when a code reads but is not the way in.
            // The finder points come from the analysis frame (image space); we map them to view space.
            // HONEST NOTE: the mapping is the part to verify on a real device , camera resolution vs
            // preview size vs rotation is the classic source of an offset overlay.
            var spin by remember { mutableStateOf(0f) }
            LaunchedEffect(granted) {
                while (granted) { spin += 3.2f; kotlinx.coroutines.delay(60) }
            }
            overlay?.let { ov ->
                Canvas(Modifier.fillMaxSize()) {
                    val pts = mapFindersToView(ov, size.width, size.height)
                    if (pts.size != 3) return@Canvas
                    val minX = pts.minOf { it.x }; val maxX = pts.maxOf { it.x }
                    val minY = pts.minOf { it.y }; val maxY = pts.maxOf { it.y }
                    val cx = (minX + maxX) / 2f; val cy = (minY + maxY) / 2f
                    // the code's span, with a margin so the aperture sits around it, not on it
                    val span = maxOf(maxX - minX, maxY - minY).coerceAtLeast(80f)
                    val radius = span * 0.85f
                    val wrong = quip != null
                    val have = frameProgress?.first ?: 0
                    val want = frameProgress?.second ?: 1
                    val lit = if (wrong) 0 else QrApertureModel.litSegments(have.coerceAtLeast(if (foundLink == null) 0 else 1), want)
                    val tint = if (wrong) AngryRed else TerminalGreen
                    val justCaptured = System.currentTimeMillis() - frameFlashAt < 350L
                    drawAperture(cx, cy, radius, tint, spin, lit, wrong, justCaptured)
                }
            }


        // Floating title at the top, clear of the status bar and the camera cutout. Sits on a dark
        // rounded pill so the green text stays legible even over a bright or greenish camera image.
        if (foundLink == null) Column(
            Modifier.align(Alignment.TopCenter).fillMaxWidth().statusBarsPadding().padding(20.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Box(
                Modifier
                    .background(Void.copy(alpha = 0.72f), MaterialTheme.shapes.small)
                    .padding(horizontal = 14.dp, vertical = 7.dp)
            ) {
                SectionLabel("SCAN THE BOX QR")
            }
        }

        // Floating panel at the bottom, over the camera, clear of the nav buttons. A dark gradient
        // scrim sits under the text so it stays legible over a bright camera image.
        if (foundLink == null) Column(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth()
                .background(
                    androidx.compose.ui.graphics.Brush.verticalGradient(
                        listOf(androidx.compose.ui.graphics.Color.Transparent, Void.copy(alpha = 0.82f), Void)
                    )
                )
                .navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 16.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            // Status and live decode diagnostic, on their own dark pill so they read over any camera
            // content. The diagnostic line is the honest feedback: it names the stage the current frame
            // reached, so a code that detects but will not decode says so ("grid found, no candidate
            // decoded") instead of just sitting there silently.
            Column(
                Modifier
                    .fillMaxWidth()
                    .background(Void.copy(alpha = 0.72f), MaterialTheme.shapes.small)
                    .padding(horizontal = 12.dp, vertical = 8.dp)
            ) {
                Text("> $status", color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
                if (tickShow) {
                    Text("  ✓ code read", color = TerminalGreen,
                        style = MaterialTheme.typography.labelMedium)
                }
                // Coaching hint: shown only during a sustained no-decode streak (see the analyser). It is
                // the actionable version of the silent technical diag , tells the person what to DO.
                coach?.let { c ->
                    Spacer(Modifier.height(4.dp))
                    Text("! $c", color = Warning, textAlign = TextAlign.Center,
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.fillMaxWidth())
                }
                // Multi-frame enrolment progress: one pip per frame, filled + checked once captured. A
                // just-captured frame briefly brightens (frameFlashAt), so each scan visibly lands.
                frameProgress?.let { (_, want) ->
                    if (want > 1) {
                        Spacer(Modifier.height(6.dp))
                        val flashing = System.currentTimeMillis() - frameFlashAt < 450L
                        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            for (seq in 1..want) {
                                val got = seq in capturedFrames
                                Text(
                                    if (got) "☑" else "☐",
                                    color = when {
                                        got && flashing -> TerminalGreen
                                        got -> TerminalGreen.copy(alpha = 0.85f)
                                        else -> GhostTextDim
                                    },
                                    style = MaterialTheme.typography.labelMedium,
                                )
                            }
                        }
                        Spacer(Modifier.height(2.dp))
                        Text(
                            "${capturedFrames.size} of $want frames , keep the phone pointed at the box",
                            color = GhostTextDim,
                            style = MaterialTheme.typography.labelSmall,
                        )
                    }
                }
                // The decoder's own commentary is for debugging (settings › debug mode); a person
                // scanning sees the coaching line above and the frame pips, nothing else.
                if (diag.isNotEmpty() && com.localghost.app.settings.AppSettings.debugMode(context)) {
                    Spacer(Modifier.height(3.dp))
                    Text("· $diag", color = GhostTextDim, style = MaterialTheme.typography.labelSmall)
                }
            }

            // A readable-but-wrong QR: the aperture over it is already red; here, one terse line
            // naming what it was, no more.
            quip?.let { g ->
                Spacer(Modifier.height(10.dp))
                Text("that is ${g.label}, not a box , point at the QR on the box",
                    color = Warning, style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.fillMaxWidth().background(VoidLighter, MaterialTheme.shapes.small).padding(12.dp))
            }

            Spacer(Modifier.height(8.dp))
            GhostButton("CANCEL / TYPE INSTEAD", onCancel, modifier = Modifier.fillMaxWidth())
        }

        // SUCCESS. A real box is scanned. The aperture the scanner held over the code blows open like
        // the unlock's iris , the lit ring races outward and fades through a bloom of green, a
        // shockwave ripples past it , and the camera is there behind it. BOX FOUND snaps in with a
        // little overshoot, the connection address types itself out, and the pinned fingerprint fills
        // in group by group , the same identity the app checks on every connection. enrolAnim (0..1
        // over the ~2.6s) drives the whole thing; celebrate is the free clock for the shimmer.
        if (foundLink != null) {
            // the machine names the CONNECTION, not its own nickname: the host the phone will reach
            // (with the port only when it is not the usual 443)
            val addr = foundLink?.let { l -> if (l.port == 443 || l.port == 0) l.host else "${l.host}:${l.port}" } ?: "the box"
            val fp = foundLink?.certFingerprint ?: ""
            val a = enrolAnim.coerceIn(0f, 1f)
            Box(Modifier.fillMaxSize()) {
                Canvas(Modifier.fillMaxSize()) {
                    val cx = size.width / 2f
                    val cy = size.height * 0.40f
                    val radius = size.minDimension * 0.30f
                    drawSuccessAperture(cx, cy, radius, celebrate, a)
                }
                Column(
                    Modifier.fillMaxSize().systemBarsPadding().padding(24.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                ) {
                    Spacer(Modifier.weight(0.60f))
                    // BOX FOUND lands with an overshoot once the iris is opening (a > ~0.4)
                    val titleIn = ((a - 0.38f) / 0.25f).coerceIn(0f, 1f)
                    val pop = if (titleIn <= 0f) 0f else 1f + 0.12f * kotlin.math.sin(titleIn * Math.PI.toFloat())
                    Column(
                        Modifier
                            .graphicsLayer { scaleX = pop; scaleY = pop; alpha = titleIn }
                            .background(Void.copy(alpha = 0.72f), MaterialTheme.shapes.small)
                            .padding(horizontal = 18.dp, vertical = 12.dp),
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Text("✓ BOX FOUND", color = TerminalGreen, style = MaterialTheme.typography.headlineSmall)
                        Spacer(Modifier.height(6.dp))
                        // the address types itself out as the iris opens
                        val shown = (addr.length * ((a - 0.45f) / 0.35f).coerceIn(0f, 1f)).toInt()
                        Text("→ " + addr.take(shown) + (if (shown < addr.length) "▋" else ""),
                            color = GhostText, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                            style = MaterialTheme.typography.bodyMedium)
                        if (fp.length >= 16) {
                            Spacer(Modifier.height(10.dp))
                            Text("pinned identity", color = TerminalDim,
                                fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                                style = MaterialTheme.typography.labelSmall)
                            // the fingerprint fills in group by group
                            val groups = groupHex(fp).split(" ")
                            val reveal = (groups.size * ((a - 0.5f) / 0.4f).coerceIn(0f, 1f)).toInt().coerceIn(0, groups.size)
                            Text(groups.take(reveal).joinToString(" "), color = TerminalGreen, textAlign = TextAlign.Center,
                                fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace,
                                style = MaterialTheme.typography.labelSmall)
                        }
                    }
                    Spacer(Modifier.weight(0.40f))
                }
            }
        }
    }
}

/** The box's fingerprint in short groups, so the pinned identity is readable on the success screen. */
private fun groupHex(fp: String): String {
    val hex = fp.filter { it.isDigit() || it in 'a'..'f' || it in 'A'..'F' }.lowercase()
    val shown = hex.take(32)
    return shown.chunked(4).joinToString(" ")
}

/**
 * The three outcomes of looking at a frame. Nothing (no readable QR, stay quiet, the person is still
 * lining up the shot), Enrol (a real localghost enrol link, proceed), or NotForUs (a QR that decoded
 * fine but is not an enrol link, where we say what it is and have an opinion). The found cases carry
 * the QR's finder points and the frame geometry so the overlay can be anchored to the actual code.
 */
private sealed interface ScanResult {
    object Nothing : ScanResult
    data class Enrol(val link: EnrollLink, val overlay: Overlay, val clean: Boolean) : ScanResult
    data class NotForUs(val guess: com.localghost.app.qr.QrGuess, val overlay: Overlay) : ScanResult
    data class Frames(val have: Int, val want: Int, val captured: Set<Int>, val justCaptured: Boolean, val overlay: Overlay) : ScanResult
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
    val corners: List<com.localghost.app.qr.QrSampler.FinderPoint>? = null,
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
): List<androidx.compose.ui.geometry.Offset> =
    mapPointsToView(ov.finders, ov.frameW, ov.frameH, ov.rotation, viewW, viewH)

/**
 * Map a list of image-space points (analysis frame) to Canvas view space, applying the frame rotation
 * then PreviewView's FILL_CENTER scale/crop. Shared by the finder overlay and the diagnostic QR outline.
 */
/**
 * Whether a sampled quad is plausibly a QR code seen in perspective. The sampler sets ScanGeom.corners
 * for ANY grid it managed to sample, including grids built from finder-shaped coincidences in ordinary
 * texture; drawing the reticle on those covers the screen in shapes where there is no code at all. A
 * real code, even tilted hard, keeps its four sides within a modest band of each other and its two
 * diagonals close; junk quads assembled from unrelated points are wildly skewed and fail one of the
 * two ratio checks. Gates only the DRAWING , detection, sampling and the fast-rate signal are untouched.
 */
private fun quadLooksSquare(q: List<com.localghost.app.qr.QrSampler.FinderPoint>): Boolean {
    if (q.size != 4) return false
    fun d(a: com.localghost.app.qr.QrSampler.FinderPoint, b: com.localghost.app.qr.QrSampler.FinderPoint): Float =
        kotlin.math.hypot((a.x - b.x).toFloat(), (a.y - b.y).toFloat())
    val sides = listOf(d(q[0], q[1]), d(q[1], q[2]), d(q[2], q[3]), d(q[3], q[0]))
    val shortest = sides.min()
    if (shortest < 1f) return false                    // degenerate
    if (sides.max() / shortest > 1.8f) return false    // ~45 degrees of tilt still passes; junk doesn't
    val d1 = d(q[0], q[2]); val d2 = d(q[1], q[3])
    return maxOf(d1, d2) / minOf(d1, d2).coerceAtLeast(1f) <= 1.45f
}

private fun mapPointsToView(
    points: List<com.localghost.app.qr.QrSampler.FinderPoint>,
    frameW: Int,
    frameH: Int,
    rotation: Int,
    viewW: Float,
    viewH: Float,
): List<androidx.compose.ui.geometry.Offset> {
    val (upW, upH) = when (rotation) {
        90, 270 -> frameH.toFloat() to frameW.toFloat()
        else -> frameW.toFloat() to frameH.toFloat()
    }
    val scale = maxOf(viewW / upW, viewH / upH)
    val dx = (viewW - upW * scale) / 2f
    val dy = (viewH - upH * scale) / 2f
    return points.map { p ->
        val (rx, ry) = when (rotation) {
            90 -> (frameH - p.y).toFloat() to p.x.toFloat()
            180 -> (frameW - p.x).toFloat() to (frameH - p.y).toFloat()
            270 -> p.y.toFloat() to (frameW - p.x).toFloat()
            else -> p.x.toFloat() to p.y.toFloat()
        }
        androidx.compose.ui.geometry.Offset(rx * scale + dx, ry * scale + dy)
    }
}

/** Mutable sampling timestamps, held in remember and touched only on the analysis thread. */
private class ScanTiming {
    var lastDecodeAt = 0L
    var lastSeenAt = 0L
    var lastDetectAt = 0L
}

/** How long a found code can be missing before found-mode drops back to hunting. */
private const val FOUND_TIMEOUT_MS = 1500L

/** How recently finders must have been detected to keep sampling at the fast (in-view) rate. Long
 *  enough to bridge a frame or two of blur while holding a code steady, short enough to drop back to
 *  the hunting rate once the code has genuinely left the frame. */
private const val DETECT_WINDOW_MS = 700L

/**
 * A clean AR "locked on" reticle: four L-shaped corner brackets at the detected quad's corners, with
 * a faint connecting outline. pulse (0..1) gently breathes the bracket length and alpha so it reads as
 * a live lock, not a static box. q is the four corners in view space (TL, TR, BR, BL order).
 */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawReticle(
    q: List<androidx.compose.ui.geometry.Offset>, color: androidx.compose.ui.graphics.Color, pulse: Float,
) {
    if (q.size != 4) return
    // faint full outline so the whole code is gently framed
    for (i in 0 until 4) {
        drawLine(color.copy(alpha = 0.25f), q[i], q[(i + 1) % 4], strokeWidth = 2f)
    }
    // bracket length is a fraction of the shorter side, breathing with the pulse
    val side = minOf(
        (q[0] - q[1]).getDistance(), (q[1] - q[2]).getDistance(),
        (q[2] - q[3]).getDistance(), (q[3] - q[0]).getDistance(),
    )
    val len = side * (0.18f + 0.05f * pulse)
    val a = 0.7f + 0.3f * pulse
    for (i in 0 until 4) {
        val p = q[i]
        val nLeft = q[(i + 3) % 4]   // previous corner
        val nRight = q[(i + 1) % 4]  // next corner
        val toL = (nLeft - p).let { it / it.getDistance() }
        val toR = (nRight - p).let { it / it.getDistance() }
        drawLine(color.copy(alpha = a), p, p + toL * len, strokeWidth = 5f)
        drawLine(color.copy(alpha = a), p, p + toR * len, strokeWidth = 5f)
    }
}


/** Pull luminance from the frame, sample candidate grids, and let our decoder pick the real one. */
// Reusable per-frame buffers. The analyser runs on a single thread, so one set of buffers can be
// reused across frames instead of allocating a ~3.7MB luminance array (and a byte array) every decode.
// Those repeated large allocations were churning the garbage collector, which both heats the phone and
// makes it stutter more the longer it runs. Buffers grow only if the frame size changes.
private object ScanBuffers {
    var lum: IntArray = IntArray(0)
    var bytes: ByteArray = ByteArray(0)
    fun lumFor(size: Int): IntArray {
        if (lum.size != size) lum = IntArray(size)
        return lum
    }
    fun bytesFor(size: Int): ByteArray {
        if (bytes.size != size) bytes = ByteArray(size)
        return bytes
    }
}

private fun tryDecode(proxy: ImageProxy, frames: com.localghost.app.qr.FrameAssembler,
                      stream: com.localghost.app.qr.StreamAssembler): ScanResult {
    return try {
        val plane = proxy.planes[0]
        val buffer = plane.buffer
        val rowStride = plane.rowStride
        val w = proxy.width
        val h = proxy.height
        val lum = ScanBuffers.lumFor(w * h)
        val data = ScanBuffers.bytesFor(buffer.remaining())
        buffer.get(data)
        for (y in 0 until h) {
            val base = y * rowStride
            val rowOut = y * w
            for (x in 0 until w) lum[rowOut + x] = data[base + x].toInt() and 0xFF
        }
        com.localghost.app.qr.QrSampler.ScanGeom.corners = null
        com.localghost.app.qr.QrSampler.ScanGeom.findersSeen = 0
        com.localghost.app.qr.QrSampler.ScanGeom.frameW = w
        com.localghost.app.qr.QrSampler.ScanGeom.frameH = h
        com.localghost.app.qr.QrSampler.ScanGeom.rotation = proxy.imageInfo.rotationDegrees

        // Sample several candidate grids (versions, alignment on/off) and let the decoder be the judge.
        // The image stage cannot tell a subtly-wrong sampling from a right one; only format BCH + Reed
        // Solomon can. We try each candidate (and the decoder itself tries all 4 rotations) and keep the
        // first that actually decodes. This is what makes tilted real frames work: the best-looking grid
        // and the decodable grid are not always the same, and only the decode settles it.
        val (candidates, diag) = QrSampler.sampleCandidates(lum, w, h)
        // f=N is the finder-cluster count this frame , the detection-vs-decode discriminator that the
        // frame dump used to answer: f=0 means the finders were never seen (detection problem), f>=3
        // with no decode means the maths downstream is what is failing.
        ScanDiag.last = "${diag.note} f=${QrSampler.ScanGeom.findersSeen}"
        if (candidates.isEmpty()) return ScanResult.Nothing

        var text: String? = null
        var overlay: Overlay? = null
        for (cand in candidates) {
            val t = try {
                QrMatrixDecode.decode(cand.grid, cand.conf)
            } catch (e: Exception) {
                null
            }
            if (t != null) {
                text = t
                overlay = Overlay(cand.finders, w, h, proxy.imageInfo.rotationDegrees,
                    com.localghost.app.qr.QrSampler.ScanGeom.corners)
                break
            }
        }
        val gN = com.localghost.app.qr.QrSampler.ScanGeom.gridN
        val ver = if (gN >= 21) (gN - 17) / 4 else 0
        val align = if (com.localghost.app.qr.QrSampler.ScanGeom.alignFound) "align+" else "align-"
        if (text == null || overlay == null) {
            ScanDiag.last = "v$ver $align f=${com.localghost.app.qr.QrSampler.ScanGeom.findersSeen}: sampled, none decoded"
            // Track a finders-but-no-decode streak on the shared ScanGeom so the composable can turn it
            // into on-screen coaching (this function is top-level, with no access to composable state).
            if (com.localghost.app.qr.QrSampler.ScanGeom.findersSeen >= 1) {
                com.localghost.app.qr.QrSampler.ScanGeom.noDecodeStreak += 1
            }
            return ScanResult.Nothing
        }
        // Whether the decode was CLEAN (plain Reed-Solomon, no erasures). A clean decode of a code that
        // parses as a valid enrol link is trustworthy on the first frame , the strict URL pattern plus a
        // well-formed pinned fingerprint make a random miscorrection into a valid enrol link effectively
        // impossible. Erasure-path decodes ("conf"/"logo") are more willing to manufacture a consistent-
        // but-wrong payload, so those still require the two-frame confirmation downstream.
        val cleanDecode = QrMatrixDecode.lastPath == "clean"
        com.localghost.app.qr.QrSampler.ScanGeom.noDecodeStreak = 0 // decoding works; clear any coaching
        ScanDiag.last = "v$ver $align ${QrMatrixDecode.lastPath} ${text.length}ch"

        // Multi-frame enrolment: a real device identity spans several QRs. If this decode is a frame,
        // feed it to the assembler and only parse once every frame is captured and the checksum verifies.
        // A single-QR (small) enrol link never matches the frame magic and falls straight through.
        val toParse: String
        if (stream.isFrame(text)) {
            // Erasure-coded set: every distinct frame counts, whichever it is. The pips show a COUNT
            // (the first `have` of K), not identities, because with parity any K of K+M do.
            val payload = stream.offer(text)
            val (have, want) = stream.progress()
            if (payload == null) {
                ScanDiag.last = "enrol frame ${have} of ${want} (any of ${stream.totalFrames()})"
                return ScanResult.Frames(have, want, (1..have).toSet(), stream.lastOfferWasNew, overlay)
            }
            ScanDiag.last = "enrol complete ${have} of ${want}"
            toParse = String(payload, Charsets.ISO_8859_1)
        } else if (frames.isFrame(text)) {
            val joined = frames.offer(text)
            val (have, want) = frames.progress()
            if (joined == null) {
                ScanDiag.last = "enrol frame ${have} of ${want} captured"
                return ScanResult.Frames(have, want, frames.capturedSeqs(), frames.lastOfferWasNew, overlay)
            }
            if (frames.lastOfferWasNew) {
                ScanDiag.last = "enrol complete ${have} of ${want}"
            }
            toParse = joined
        } else {
            // Not a well-formed frame. But if we are MID-COLLECTION (some frames captured, not all), a
            // decode that is not a clean frame is almost always a garbled candidate of one , the decoder
            // tries several samplings per physical QR and a bad one can lose the "LGQR1" prefix. Showing
            // "not an enrol link" for it is wrong and alarming. Stay in frame mode and keep the progress
            // UI up rather than routing a near-miss to the wrong-QR classifier.
            val (have, want) = frames.progress()
            if (want > 0 && have < want) {
                return ScanResult.Frames(have, want, frames.capturedSeqs(), false, overlay)
            }
            val (shave, swant) = stream.progress()
            if (swant > 0 && shave < swant) {
                return ScanResult.Frames(shave, swant, (1..shave).toSet(), false, overlay)
            }
            toParse = text
        }

        when (val r = EnrollLink.parseResult(toParse)) {
            is EnrollLink.Result.Ok -> ScanResult.Enrol(r.link, overlay, cleanDecode)
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
        // A scanned code must never crash the app. Any throwable becomes "no readable QR" and the
        // person keeps scanning or types the values. A bad scan can never enrol anyway, because the
        // fingerprint pin must still match.
        ScanDiag.last = "frame error: ${t.message ?: t.javaClass.simpleName}"
        ScanResult.Nothing
    }
}

/** Thread-safe holder for the latest scan-pipeline diagnostic, read by the status line. */
private object ScanDiag {
    @Volatile var last: String = "starting"
}

// --- the vault aperture drawn over the code, in the unlock's language (QrApertureModel) ---

/**
 * The aperture around a code the scanner is reading. A ring of twelve segments: the lit ones bright,
 * the rest a faint outline, so a multi-frame enrolment fills the ring as its frames land. A scan tick
 * sweeps the ring as it turns. Red (wrong == true) when a code read but is not the way in. On a fresh
 * frame capture (justCaptured) the whole ring flares for a beat.
 */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawAperture(
    cx: Float, cy: Float, radius: Float,
    tint: androidx.compose.ui.graphics.Color, spin: Float, lit: Int, wrong: Boolean, justCaptured: Boolean,
) {
    val centre = androidx.compose.ui.geometry.Offset(cx, cy)
    val segs = QrApertureModel.SEGMENTS
    val gap = 6f                       // degrees of gap between segments
    val sweep = 360f / segs - gap
    val stroke = radius * 0.10f
    val topLeft = androidx.compose.ui.geometry.Offset(cx - radius, cy - radius)
    val arcSize = androidx.compose.ui.geometry.Size(radius * 2, radius * 2)
    val flare = if (justCaptured) 0.35f else 0f
    for (i in 0 until segs) {
        val start = QrApertureModel.segmentAngle(i, segs) - sweep / 2f + spin * 0.15f
        val on = i < lit
        val alpha = when {
            on -> (0.85f + flare).coerceAtMost(1f)
            else -> 0.16f
        }
        drawArc(
            color = tint.copy(alpha = alpha),
            startAngle = start, sweepAngle = sweep, useCenter = false,
            topLeft = topLeft, size = arcSize,
            style = androidx.compose.ui.graphics.drawscope.Stroke(width = if (on) stroke else stroke * 0.5f,
                cap = androidx.compose.ui.graphics.StrokeCap.Round),
        )
    }
    // a bright scan tick that sweeps the ring while it reads (not when wrong)
    if (!wrong) {
        val a = Math.toRadians((spin % 360f - 90f).toDouble())
        val p = androidx.compose.ui.geometry.Offset(cx + (radius * kotlin.math.cos(a)).toFloat(),
            cy + (radius * kotlin.math.sin(a)).toFloat())
        drawCircle(tint, radius * 0.06f, p)
    }
    // a faint corner-crosshair in the middle so the code is clearly the target
    val c = radius * 0.16f
    val cw = radius * 0.03f
    for (s in listOf(-1f, 1f)) {
        drawLine(tint.copy(alpha = 0.5f), androidx.compose.ui.geometry.Offset(cx + s * c, cy),
            androidx.compose.ui.geometry.Offset(cx + s * c * 0.4f, cy), cw)
        drawLine(tint.copy(alpha = 0.5f), androidx.compose.ui.geometry.Offset(cx, cy + s * c),
            androidx.compose.ui.geometry.Offset(cx, cy + s * c * 0.4f), cw)
    }
}

/**
 * The success aperture, in the unlock's iris language. Over [t] (0..1, the whole ~2.6s):
 *   0.00..0.40  the twelve segments finish, hold lit, and give one bright pulse , the lock is made;
 *   0.40..1.00  the whole lit ring blows OPEN , it scales outward and fades while a green bloom
 *               swells from the centre and a bright shockwave ring races out past it, the camera
 *               there behind the opening.
 * shimmer is a free clock for a faint breathe on the held ring. Nothing is drawn opaque , the AR
 * camera stays visible throughout, which is the whole point.
 */
private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawSuccessAperture(
    cx: Float, cy: Float, radius: Float, shimmer: Float, t: Float,
) {
    val centre = androidx.compose.ui.geometry.Offset(cx, cy)
    val tint = TerminalGreen
    val segs = QrApertureModel.SEGMENTS
    val gap = 4f
    val sweep = 360f / segs - gap
    val hold = (t / 0.40f).coerceIn(0f, 1f)         // 0..1 over the make
    val open = ((t - 0.40f) / 0.60f).coerceIn(0f, 1f) // 0..1 over the blow-open
    val eased = 1f - (1f - open) * (1f - open)         // ease-out, like the unlock iris

    // the lit ring: held tight while the lock is made, then scaling out (1x -> 2.4x) and fading
    val pulse = if (t < 0.42f) 0.85f + 0.15f * kotlin.math.sin(shimmer * 3f) else 1f
    val r = radius * (1f - 0.08f * hold) * (1f + 1.4f * eased)
    val ringAlpha = (1f - eased) * pulse
    if (ringAlpha > 0.01f) {
        val stroke = radius * 0.11f * (1f - 0.4f * eased)
        val topLeft = androidx.compose.ui.geometry.Offset(cx - r, cy - r)
        val arcSize = androidx.compose.ui.geometry.Size(r * 2, r * 2)
        for (i in 0 until segs) {
            val start = QrApertureModel.segmentAngle(i, segs) - sweep / 2f + eased * 24f // a slight twist as it opens
            drawArc(color = tint.copy(alpha = 0.9f * ringAlpha), startAngle = start, sweepAngle = sweep, useCenter = false,
                topLeft = topLeft, size = arcSize,
                style = androidx.compose.ui.graphics.drawscope.Stroke(width = stroke, cap = androidx.compose.ui.graphics.StrokeCap.Round))
        }
    }

    // the bloom from the centre , brightest at the moment of opening, then gone
    val bloom = kotlin.math.sin(eased * Math.PI.toFloat())
    if (bloom > 0.01f) {
        val br = radius * (0.6f + 1.6f * eased)
        drawCircle(
            brush = androidx.compose.ui.graphics.Brush.radialGradient(
                0.0f to tint.copy(alpha = 0.28f * bloom),
                0.6f to tint.copy(alpha = 0.10f * bloom),
                1.0f to androidx.compose.ui.graphics.Color.Transparent,
                center = centre, radius = br),
            radius = br, center = centre)
    }

    // the shockwave: a bright thin ring racing out past the opening
    if (open > 0.01f) {
        val wr = radius * (0.4f + 3.0f * eased)
        drawCircle(color = tint.copy(alpha = (1f - eased) * 0.7f), radius = wr, center = centre,
            style = androidx.compose.ui.graphics.drawscope.Stroke(width = radius * 0.05f * (1f - eased)))
    }
}
