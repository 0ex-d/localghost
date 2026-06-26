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
import androidx.compose.foundation.layout.*
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
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

    val permLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { ok -> granted = ok }

    LaunchedEffect(Unit) {
        if (!granted) permLauncher.launch(Manifest.permission.CAMERA)
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

        Box(Modifier.weight(1f).fillMaxWidth()) {
            AndroidView(factory = { ctx ->
                val previewView = PreviewView(ctx)
                val providerFuture = ProcessCameraProvider.getInstance(ctx)
                providerFuture.addListener({
                    val provider = providerFuture.get()
                    val preview = androidx.camera.core.Preview.Builder().build().also {
                        it.surfaceProvider = previewView.surfaceProvider
                    }
                    val analysis = ImageAnalysis.Builder()
                        .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                        .build()
                    analysis.setAnalyzer(analysisExecutor) { proxy ->
                        val link = tryDecode(proxy)
                        proxy.close()
                        if (link != null) {
                            status = "found ${link.host}"
                            onLink(link)
                        }
                    }
                    provider.unbindAll()
                    provider.bindToLifecycle(
                        lifecycleOwner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis
                    )
                }, ContextCompat.getMainExecutor(ctx))
                previewView
            }, modifier = Modifier.fillMaxSize())
        }

        Spacer(Modifier.height(12.dp))
        Text("> $status", color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
        Spacer(Modifier.height(8.dp))
        GhostButton("CANCEL / TYPE INSTEAD", onCancel, modifier = Modifier.fillMaxWidth())
    }
}

/** Pull luminance from the frame, sample a grid, decode, and parse an enrol link. Null on any miss. */
private fun tryDecode(proxy: ImageProxy): EnrollLink? {
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
        val grid = QrSampler.sample(lum, w, h) ?: return null
        val text = QrMatrixDecode.decode(grid)
        if (!text.startsWith("localghost://")) return null
        EnrollLink.parse(text)
    } catch (e: Exception) {
        null
    }
}
