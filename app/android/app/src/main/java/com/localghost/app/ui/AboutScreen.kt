package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.localghost.app.ui.theme.GhostText
import com.localghost.app.ui.theme.GhostTextDim
import com.localghost.app.ui.theme.TerminalGreen

@Composable
fun AboutScreen() {
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(20.dp).padding(bottom = 24.dp)) {
        SectionLabel("ABOUT")
        Spacer(Modifier.height(16.dp))
        Text("LOCALGHOST", color = TerminalGreen, style = MaterialTheme.typography.titleLarge)
        Spacer(Modifier.height(8.dp))
        Text("> the only cloud is you", color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
        Spacer(Modifier.height(24.dp))
        Text("All processing and storage runs on hardware you own. This app is a " +
             "thin client: it authenticates over mTLS, syncs media, and reads results. It " +
             "holds no index and no model. If you lose the phone, the data is on the box, not " +
             "in your pocket.",
             color = GhostText, style = MaterialTheme.typography.bodyMedium)

        Spacer(Modifier.height(24.dp))
        SectionLabel("ACKNOWLEDGEMENTS")
        Spacer(Modifier.height(8.dp))
        Text("The QR scanner is our own implementation, with no third-party barcode library. Its " +
             "geometry , finder detection, perspective sampling, and grid extraction , was inspired " +
             "by the ZXing project (github.com/zxing/zxing, Apache-2.0). We ported the approach " +
             "rather than the code, and we are grateful for their work.",
             color = GhostText, style = MaterialTheme.typography.bodyMedium)
    }
}
