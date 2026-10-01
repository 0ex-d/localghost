package com.localghost.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.unit.dp
import com.localghost.app.local.MapPrefetch
import com.localghost.app.net.BoxClient
import com.localghost.app.settings.AppSettings
import com.localghost.app.ui.theme.*

/**
 * WHOLE COUNTRIES ON THE PHONE. Under SETTINGS › MAPS: the countries picked, each with how many of
 * its tiles are here, and a picker that lists every country with what the box holds for it (the
 * streets, the main roads, the coast, and the size), so the choice is made knowing the cost. A
 * country the box has nothing for cannot be picked; it says so instead. Picking one starts a
 * download on this network; the daily run keeps it complete as the box cuts more.
 */
@Composable
fun MapCountriesSection(mapTick: Int, onChanged: () -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val picked = remember(mapTick) { AppSettings.mapCountries(ctx) }
    val progress = remember(mapTick) { MapPrefetch.countryProgress(ctx) }
    var open by remember { mutableStateOf(false) }
    var list by remember { mutableStateOf<BoxClient.Countries?>(null) }
    var failed by remember { mutableStateOf(false) }
    var filter by remember { mutableStateOf("") }
    LaunchedEffect(open) {
        if (open && list == null) {
            failed = false
            list = BoxClient.countries(ctx)
            if (list == null) failed = true
        }
    }
    Spacer(Modifier.height(8.dp))
    Text("WHOLE COUNTRIES", color = TerminalDim, style = MaterialTheme.typography.labelSmall)
    if (picked.isEmpty()) {
        Text("none picked , every street, main road and stretch of coast the box holds for a country, kept here outside the size above",
            color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
    }
    for (code in picked) {
        val p = progress.firstOrNull { it.code == code }
        val name = p?.name ?: list?.rows?.firstOrNull { it.code == code }?.name ?: code
        Row(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
            Text(if (p == null) "$name · waiting for the next run to count its tiles"
                 else MapCountryText.progress(p.name, p.have, p.total, p.bytes),
                color = if (p != null && p.have >= p.total && p.total > 0) TerminalGreen else GhostTextDim,
                style = MaterialTheme.typography.labelMedium, modifier = Modifier.weight(1f))
            Text("[ x ]", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    AppSettings.setMapCountries(ctx, picked - code)
                    MapPrefetch.forgetCountry(ctx, code)
                    onChanged()
                }.padding(start = 8.dp))
        }
    }
    Text(if (open) "[ close the list ]" else "[ pick countries ]", color = TerminalGreen,
        style = MaterialTheme.typography.labelMedium,
        modifier = Modifier.clickable { open = !open }.padding(vertical = 6.dp))
    if (open) {
        val l = list
        when {
            failed -> Text("! the box did not answer , is it unlocked?", color = Warning, style = MaterialTheme.typography.labelMedium)
            l == null -> Text("reading from the box…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            l.rows.isEmpty() -> Text("the box has no world file (geo/world.geojson) , nothing to pick from",
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            else -> {
                if (!l.roads) Text("> the box has not cut any roads yet (tools/fetch_geo.sh with GHOST_GEO_ROADS, then ghost.framed road-tiles)",
                    color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                Row(
                    Modifier.fillMaxWidth().padding(vertical = 6.dp)
                        .border(1.dp, GhostBorder, RoundedCornerShape(16.dp))
                        .background(VoidLighter, RoundedCornerShape(16.dp))
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                ) {
                    BasicTextField(
                        value = filter, onValueChange = { filter = it }, singleLine = true,
                        modifier = Modifier.weight(1f),
                        textStyle = MaterialTheme.typography.labelMedium.copy(color = GhostText),
                        cursorBrush = SolidColor(TerminalGreen),
                        decorationBox = { inner ->
                            if (filter.isEmpty()) Text("find a country…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                            inner()
                        },
                    )
                    if (filter.isNotEmpty()) Text("✕", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { filter = "" }.padding(start = 8.dp))
                }
                val rows = remember(l, filter) {
                    val q = filter.trim()
                    if (q.isEmpty()) l.rows else l.rows.filter { it.name.contains(q, ignoreCase = true) || it.code.equals(q, ignoreCase = true) }
                }
                // a fixed-height list inside the scrolling screen: the whole world is 250 rows
                LazyColumn(Modifier.fillMaxWidth().height(300.dp).border(1.dp, GhostBorder)) {
                    items(rows, key = { it.code }) { r ->
                        val on = r.code in picked
                        val can = r.streets + r.major + r.coast > 0
                        Row(Modifier.fillMaxWidth()
                            .clickable(enabled = can) {
                                AppSettings.setMapCountries(ctx, if (on) picked - r.code else picked + r.code)
                                if (on) MapPrefetch.forgetCountry(ctx, r.code)
                                else if (AppSettings.mapDownload(ctx)) MapPrefetch.runNow(ctx)
                                onChanged()
                            }
                            .padding(horizontal = 10.dp, vertical = 7.dp)) {
                            Text(if (on) "[x] " else if (can) "[ ] " else "    ",
                                color = if (on) TerminalGreen else GhostTextDim, style = MaterialTheme.typography.labelMedium)
                            Text(MapCountryText.row(r.name, r.streets, r.major, r.coast, r.bytes),
                                color = when { on -> TerminalGreen; can -> GhostText; else -> TerminalDim },
                                style = MaterialTheme.typography.labelMedium)
                        }
                    }
                }
                Text("> a picked country downloads on this network now when downloads are on, and the daily run keeps it complete",
                    color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}
