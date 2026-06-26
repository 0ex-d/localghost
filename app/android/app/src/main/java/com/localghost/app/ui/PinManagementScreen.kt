package com.localghost.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.unit.dp
import com.localghost.app.net.DeviceInfo
import com.localghost.app.ui.theme.*

/**
 * The CODES screen. PIN management was deliberately removed from the app: a PIN can only be changed
 * or reset at the box, over a local-network SSH session, never from a phone. This is so a coerced
 * phone cannot change or reset a PIN and lock you out or take over the account. The screen now shows
 * the enrolled devices and points to the box commands for PIN changes.
 */
@Composable
fun PinManagementScreen(
    devices: Loadable<List<DeviceInfo>>,
) {
    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Spacer(Modifier.height(12.dp))
            SectionLabel("CODES")
            Spacer(Modifier.height(8.dp))
            Text("Codes are managed at the box, not in the app. To change a code (keeps your data) " +
                 "run `ghost.secd changepin-<slot>`; to reset one (wipes that slot and starts " +
                 "fresh) run `ghost.secd resetup-<slot>`. Both work only over a local-network SSH " +
                 "session, so a phone, even unlocked under coercion, cannot change or reset a code.",
                 color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(20.dp))
            SectionLabel("DEVICES")
            Spacer(Modifier.height(8.dp))
            Text("Each device enrols separately and syncs on its own cursor. The box dedups " +
                 "by content, so the same photo from two devices is one memory.",
                 color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
            Spacer(Modifier.height(6.dp))
        }
        when (devices) {
            is Loadable.Loading -> item { LoadingRow("reading devices…") }
            is Loadable.Failed -> item { ErrorLine(devices.reason) }
            is Loadable.Loaded -> items(devices.value) { d -> DeviceRow(d) }
        }
        item { Spacer(Modifier.height(24.dp)) }
    }
}

@Composable
private fun DeviceRow(d: DeviceInfo) {
    Column(Modifier.fillMaxWidth().border(1.dp, GhostBorder, RectangleShape)
        .background(VoidLighter).padding(14.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(d.name, color = TerminalGreen, style = MaterialTheme.typography.titleMedium)
            if (d.thisDevice) {
                Spacer(Modifier.width(8.dp))
                Text("[ this device ]", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            }
        }
        Spacer(Modifier.height(4.dp))
        Text("${d.photos} photos · ${d.videos} videos · last sync ${d.lastSync}",
            color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
    }
}
