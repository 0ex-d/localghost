package com.localghost.app.ui

import androidx.compose.foundation.border
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.localghost.app.ui.theme.*
import kotlinx.coroutines.launch

@Composable
fun SettingsScreen(
    onOpenVerify: () -> Unit = {},
    onOpenMap: () -> Unit = {},
    allowMobileSync: Boolean,
    onToggleMobileSync: (Boolean) -> Unit,
    thinkLevel: String = "",
    onCycleThink: () -> Unit = {},
    notificationsMuted: Boolean,
    onToggleMute: (Boolean) -> Unit,
    onExport: () -> Unit,
    exportState: String?,
    onLock: () -> Unit,
    onWipe: () -> Unit,
) {
    // ONE SCREEN, IN THE ORDER THINGS MATTER: the box first (what it runs, updates, lock), then
    // what the phone sends it (photos, the trail, health), then what the phone keeps for itself
    // (maps), then how the box answers (chat), then the rest. Each part folds; the closed line
    // says its state, so the screen reads at a glance and opens only where you are going.
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())
        .padding(20.dp).padding(bottom = 24.dp)) {
        SectionLabel("SETTINGS")
        Spacer(Modifier.height(12.dp))

        Fold("YOUR BOX", "the build it runs, updates, lock, verify", openAtFirst = true) {
            ServerUpdateSection(onLock)
            Spacer(Modifier.height(16.dp))
            Spacer(Modifier.height(8.dp))
            GhostButton("LOCK BOX NOW", onLock, modifier = Modifier.fillMaxWidth())
            Spacer(Modifier.height(4.dp))
            Text("Spins the box down: stops the databases, unmounts the drive, and drops the key from " +
                 "memory. The box goes dark until you enter your PIN again. Your data is untouched.",
                 color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            Spacer(Modifier.height(16.dp))
            Spacer(Modifier.height(8.dp))
            GhostButton("VERIFY BUILD ✓", onOpenVerify, modifier = Modifier.fillMaxWidth())
            Spacer(Modifier.height(4.dp))
            Text("Checks that what the box is running matches the public source. An audit action, not " +
                 "a daily one , which is why it lives here instead of taking a menu slot.",
                 color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }

        Fold("PHOTOS AND FILES", if (allowMobileSync) "sync on Wi-Fi and mobile data" else "sync on Wi-Fi only", openAtFirst = false) {
            toggleRow(
                label = "sync over mobile data",
                sub = if (allowMobileSync) "on, uses Wi-Fi and mobile (4G/5G)"
                      else "off, Wi-Fi only (recommended)",
                checked = allowMobileSync, onChange = onToggleMobileSync,
            )
        }

        Fold("LOCATION TRAIL", "where you have been, a fix every quarter hour", openAtFirst = true) {
            Spacer(Modifier.height(8.dp))
            // Read and written right here, like the phrase switches: this is per-phone state, and the
            // shell has no reason to carry it.
            var trailTick by remember { mutableIntStateOf(0) }
            val trailOn = remember(trailTick) { com.localghost.app.settings.AppSettings.locationTrail(ctx) }
            val trailAllowed = remember(trailTick) { com.localghost.app.sync.LocationLog.hasPermission(ctx) }
            val trailBackground = remember(trailTick) { com.localghost.app.sync.LocationLog.hasBackground(ctx) }
            val waiting = remember(trailTick) { com.localghost.app.sync.LocationLog.pendingCount(ctx) }
            val today = remember(trailTick) { com.localghost.app.sync.LocationLog.countToday(ctx) }
            val passive = remember(trailTick) { com.localghost.app.sync.LocationLog.passiveToday(ctx) }
            val sealedTo = remember(trailTick) { com.localghost.app.sync.TrailKeys.where(ctx) }
            // the counts are the lines under the switch (TrailStatus); the switch says how it is kept
            @Suppress("UNUSED_VARIABLE") val passiveSeen = passive
            val sealedLine = when (sealedTo) {
                "box" -> " · sealed on this phone, opened only by your box PIN"
                "phone" -> " · sealed on this phone, opened by the phone's own unlock"
                else -> ""
            }
            toggleRow(
                label = "keep the trail",
                sub = when {
                    !trailOn -> "off, the phone takes no fixes"
                    !trailAllowed -> "on, but location is not allowed for LocalGhost , nothing is recorded"
                    !trailBackground -> "on while the app is open only ('always' not allowed)"
                    waiting > 0 -> "on, a point every quarter hour plus other apps' fixes$sealedLine · $waiting waiting for the box"
                    else -> "on, a point every quarter hour plus other apps' fixes$sealedLine · all on the box"
                },
                checked = trailOn,
                onChange = { on ->
                    com.localghost.app.settings.AppSettings.setLocationTrail(ctx, on)
                    if (on) com.localghost.app.sync.LocationLog.schedule(ctx) else com.localghost.app.sync.LocationLog.stop(ctx)
                    trailTick++
                },
            )
            // WHERE THE TRAIL IS: the phone's newest fix it can read, and how the last hand-over to
            // the box went. "It says no position" and "is it reaching the box" are both answered here.
            if (trailOn) {
                val newest = remember(trailTick) { com.localghost.app.sync.LocationLog.newest(ctx) }
                val send = remember(trailTick) { com.localghost.app.sync.LocationLog.lastSend(ctx) }
                val now = System.currentTimeMillis() / 1000
                val lastState = remember(trailTick) { com.localghost.app.sync.LocationLog.lastState(ctx) }
                Text(TrailStatus.fixLine(newest?.ts, now, lastState), color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                val byVia = remember(trailTick) { com.localghost.app.sync.LocationLog.todayByVia(ctx) }
                val sentToday = remember(trailTick) { com.localghost.app.sync.LocationLog.sentToday(ctx) }
                val sentTotal = remember(trailTick) { com.localghost.app.sync.LocationLog.sentTotal(ctx) }
                val ring = remember(trailTick) {
                    if (com.localghost.app.sync.TrailKeys.opener() != null) com.localghost.app.sync.LocationLog.recent(ctx).size else null
                }
                Text(TrailStatus.todayLine(today, byVia, sentToday, waiting), color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                Text(TrailStatus.holdsLine(ring, sentTotal), color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                Text(TrailStatus.sendLine(send?.at, send?.what, send?.ok, send?.lastOkAt, waiting, now),
                    color = if (send != null && !send.ok && waiting > 0) Warning else GhostTextDim, style = MaterialTheme.typography.labelMedium)
                Text("[ send the trail to the box now ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable {
                        scope.launch {
                            kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { com.localghost.app.sync.LocationLog.flush(ctx) }
                            trailTick++
                        }
                    }.padding(vertical = 6.dp))
            }
            // The trail is drawn on the map , by day, with a clock along the line , and the switch
            // that records it lives here; one tap joins the two.
            Text("[ see the trail on the map ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { onOpenMap() }.padding(vertical = 6.dp))
        }

        Fold("HEALTH", "steps, sleep, heart rate from Health Connect, to the box", openAtFirst = true) {
            HealthSection()
        }

        Fold("MAPS ON THIS PHONE", "tiles kept ahead of time", openAtFirst = false) {
            Spacer(Modifier.height(8.dp))
            // The map fetches tiles from the box as you look; ticked, the phone keeps them ahead of
            // time (Wi-Fi only, once a day), streets around where you have been first.
            var mapTick by remember { mutableIntStateOf(0) }
            val mapsOn = remember(mapTick) { com.localghost.app.settings.AppSettings.mapDownload(ctx) }
            val mapBudget = remember(mapTick) { com.localghost.app.settings.AppSettings.mapBudgetMB(ctx) }
            // the status line follows a run as it goes (the worker notes its progress every ten tiles)
            var mapStatus by remember { mutableStateOf("") }
            LaunchedEffect(mapTick) {
                while (true) {
                    mapStatus = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
                        com.localghost.app.local.MapPrefetch.statusLine(ctx)
                    }
                    kotlinx.coroutines.delay(3_000)
                }
            }
            toggleRow(
                label = "download maps",
                sub = if (mapsOn) "on Wi-Fi, once a day: streets around where you have been, then the coast and main roads outwards · $mapStatus"
                    else "off , tiles come from the box as you look (slow the first time anywhere) · $mapStatus",
                checked = mapsOn,
                onChange = { on ->
                    com.localghost.app.settings.AppSettings.setMapDownload(ctx, on)
                    if (on) { com.localghost.app.local.MapPrefetch.schedule(ctx); com.localghost.app.local.MapPrefetch.runNow(ctx) }
                    else com.localghost.app.local.MapPrefetch.cancel(ctx)
                    mapTick++
                },
            )
            if (mapsOn) {
                Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.padding(vertical = 4.dp)) {
                    Text("keep up to", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    listOf(250, 500, 1000, 2000).forEach { mb ->
                        Text(if (mb >= 1000) "${mb / 1000} GB" else "$mb MB",
                            color = if (mb == mapBudget) TerminalGreen else GhostTextDim,
                            style = MaterialTheme.typography.labelMedium,
                            modifier = Modifier.clickable {
                                com.localghost.app.settings.AppSettings.setMapBudgetMB(ctx, mb); mapTick++
                            }.padding(horizontal = 8.dp, vertical = 6.dp))
                    }
                }
                Text("[ download now ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { com.localghost.app.local.MapPrefetch.runNow(ctx); mapTick++ }.padding(vertical = 6.dp))
            }
        }

        Fold("CHAT", "thinking " + thinkLevel.ifEmpty { "off" } + " · web search", openAtFirst = false) {
            Spacer(Modifier.height(8.dp))
            // Deliberation depth for every chat answer. Tapping cycles off -> brief -> deep. Honest
            // mechanics: this asks the model to show its working (and gives it a bigger token budget) ,
            // deeper means slower, especially on CPU.
            Row(Modifier.fillMaxWidth().clickable { onCycleThink() }.padding(vertical = 8.dp)) {
                Column(Modifier.weight(1f)) {
                    Text("thinking", color = GhostText, style = MaterialTheme.typography.bodyLarge)
                    Text(when (thinkLevel) {
                        "brief" -> "brief , a few lines of reasoning first (slower)"
                        "deep" -> "deep , thorough reasoning first (much slower)"
                        else -> "off , answers directly (fastest)"
                    }, color = GhostTextDim, style = MaterialTheme.typography.bodySmall)
                }
                Text(when (thinkLevel) { "brief" -> "[ BRIEF ]"; "deep" -> "[ DEEP ]"; else -> "[ OFF ]" },
                    color = TerminalGreen, style = MaterialTheme.typography.bodyMedium)
            }

            Spacer(Modifier.height(16.dp))
            // WEB SEARCH , the engine the PHONE uses when a question goes to the web (the box never
            // does). DuckDuckGo needs nothing and is scraped HTML, which it sometimes answers with a
            // bot check; Brave is a real API with the person's own key and DuckDuckGo behind it.
            // Google offers neither: its results page forbids scripts, and its search API is closed
            // to new customers and ends on 1 January 2027.
            var engine by remember { mutableStateOf(com.localghost.app.settings.AppSettings.searchEngine(ctx)) }
            var braveKey by remember { mutableStateOf(com.localghost.app.settings.AppSettings.braveKey(ctx)) }
            Text("web search engine", color = GhostText, style = MaterialTheme.typography.bodyLarge)
            Text(if (engine == "brave") (if (braveKey.isBlank()) "Brave , paste your API key below; until then DuckDuckGo answers" else "Brave with your key , DuckDuckGo if it fails")
                else "DuckDuckGo , no key, nothing to set up",
                color = GhostTextDim, style = MaterialTheme.typography.bodySmall)
            Spacer(Modifier.height(6.dp))
            Row {
                for ((id, label) in listOf("duckduckgo" to "DuckDuckGo", "brave" to "Brave (your key)")) {
                    val on = engine == id
                    Text(label, color = if (on) Void else TerminalGreen, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.padding(end = 8.dp)
                            .border(1.dp, TerminalGreen, androidx.compose.ui.graphics.RectangleShape)
                            .background(if (on) TerminalGreen else Void)
                            .clickable { engine = id; com.localghost.app.settings.AppSettings.setSearchEngine(ctx, id) }
                            .padding(horizontal = 10.dp, vertical = 6.dp))
                }
            }
            if (engine == "brave") {
                Spacer(Modifier.height(8.dp))
                androidx.compose.foundation.text.BasicTextField(braveKey, {
                    braveKey = it.trim(); com.localghost.app.settings.AppSettings.setBraveKey(ctx, braveKey)
                }, singleLine = true,
                    textStyle = MaterialTheme.typography.bodySmall.copy(color = GhostText),
                    cursorBrush = androidx.compose.ui.graphics.SolidColor(TerminalGreen),
                    visualTransformation = if (braveKey.isEmpty()) androidx.compose.ui.text.input.VisualTransformation.None
                        else androidx.compose.ui.text.input.PasswordVisualTransformation(),
                    decorationBox = { inner -> Box(Modifier.fillMaxWidth()
                        .border(1.dp, GhostBorder, androidx.compose.ui.graphics.RectangleShape).padding(8.dp)) {
                        if (braveKey.isEmpty()) Text("Brave Search API key (api-dashboard.search.brave.com)", color = TerminalDim,
                            style = MaterialTheme.typography.bodySmall); inner() } },
                    modifier = Modifier.fillMaxWidth())
                Text("kept on this phone only · the box never sees it · Brave charges per 1,000 searches after a monthly free credit",
                    color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            }
        }

        Fold("NOTIFICATIONS", if (notificationsMuted) "muted on this phone" else "on", openAtFirst = false) {
            Spacer(Modifier.height(8.dp))
            toggleRow(
                label = "daemon notifications",
                sub = if (notificationsMuted) "muted, daemons stay silent"
                      else "active, daemons can notify you",
                checked = !notificationsMuted, onChange = { on -> onToggleMute(!on) },
            )
        }

        Fold("PHRASES", "the language around you, on the lock screen", openAtFirst = false) {
            Spacer(Modifier.height(8.dp))
            // Off until the phone lands somewhere that is not home and the person says yes; this is the
            // manual way in (and out), and where home is set , the SIM's country by default.
            var phraseTick by remember { mutableIntStateOf(0) }
            val phrasesOn = remember(phraseTick) { com.localghost.app.phrases.PhraseState.enabled(ctx) }
            val home = remember(phraseTick) { com.localghost.app.phrases.PhraseOffer.homeCountry(ctx) }
            val here = remember(phraseTick) { com.localghost.app.phrases.CountryDetect.detect(ctx) }
            toggleRow(
                label = "phrases on the lock screen",
                sub = if (phrasesOn) "on · the phrase of the hour in the language around you · PHRASES is in the drawer"
                    else "off · offered once when you land somewhere that is not home; this switch is the other way in",
                checked = phrasesOn,
                onChange = { on ->
                    if (on) com.localghost.app.phrases.PhraseOffer.accept(ctx)
                    else {
                        com.localghost.app.phrases.PhraseState.setEnabled(ctx, false)
                        com.localghost.app.phrases.PhraseState.setLockScreenOn(ctx, false)
                        Thread { com.localghost.app.phrases.PhraseSurface.refresh(ctx) }.start()
                    }
                    phraseTick++
                },
            )
            Spacer(Modifier.height(8.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("home", color = GhostText, style = MaterialTheme.typography.bodyMedium)
                    Text((if (home.isEmpty()) "not set , the SIM has no country" else com.localghost.app.phrases.CountryNames.of(home)) +
                        " · the phrases never offer themselves here" +
                        (if (here.country.isNotEmpty() && here.country != home) " · you seem to be in ${com.localghost.app.phrases.CountryNames.of(here.country)} (${here.source})" else ""),
                        color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                }
                if (here.country.isNotEmpty() && here.country != home) {
                    Text("[ home is here ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { com.localghost.app.settings.AppSettings.setHomeCountry(ctx, here.country); phraseTick++ }.padding(6.dp))
                }
            }
        }

        Fold("YOUR DATA", "export (not built yet), PIN changes", openAtFirst = false) {
            Spacer(Modifier.height(8.dp))
            Text("The box holds the index. The phone holds nothing.",
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            Spacer(Modifier.height(12.dp))
            // honest: the export is not built on the box yet (it used to hand over a made-up
            // file); the button says so instead of pretending
            @Suppress("UNUSED_EXPRESSION") onExport
            @Suppress("UNUSED_EXPRESSION") exportState
            Text("export to JSON: not built yet , your originals are on the box's encrypted volume as ordinary files, and `tools/backup` on the box makes a sealed copy",
                color = TerminalDim, style = MaterialTheme.typography.labelMedium)

            Spacer(Modifier.height(12.dp))
            Text("PIN changes happen at the box, not in the app. Run `ghost.secd changepin-<slot>` " +
                 "(keeps your data) or `ghost.secd resetup-<slot>` (wipes and starts fresh) over a " +
                 "local-network SSH session. A coerced phone cannot change or reset a PIN.",
                 color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }

        Fold("DEVELOPER", "debug mode", openAtFirst = false) {
            var dbg by remember { mutableStateOf(com.localghost.app.settings.AppSettings.debugMode(ctx)) }
            Text(if (dbg) "[x] app is in DEBUG MODE , tap to disable" else "[ ] set app in debug mode",
                color = if (dbg) TerminalGreen else GhostTextDim,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.clickable {
                    dbg = !dbg
                    com.localghost.app.settings.AppSettings.setDebugMode(ctx, dbg)
                }.padding(vertical = 6.dp))
            Text("> shows the tok/s meter under chat replies, and the map's graticule",
                color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        }

        Fold("DANGER", "wipe this phone", openAtFirst = false) {
            Spacer(Modifier.height(8.dp))
            WipeButton(onWipe)
            Spacer(Modifier.height(4.dp))
            Text("Forgets the box on THIS PHONE: the enrolment, the certificate, the session. The box " +
                 "and everything on it are untouched; a crypto-erase of the box is done at the box " +
                 "(ghost.secd resetup), never from a phone.",
                 color = Warning, style = MaterialTheme.typography.labelMedium)
        }

        Spacer(Modifier.height(28.dp))
        Text("> the only cloud is you", color = GhostTextDim,
            style = MaterialTheme.typography.labelMedium)
    }
}

/** A part of the screen that folds: the label, a one-line state when closed, the body when open. */
@Composable
private fun Fold(label: String, closedLine: String, openAtFirst: Boolean, body: @Composable () -> Unit) {
    var open by remember { mutableStateOf(openAtFirst) }
    Spacer(Modifier.height(6.dp))
    Row(Modifier.fillMaxWidth().clickable { open = !open }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(if (open) "▾" else "▸", color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
        Spacer(Modifier.width(8.dp))
        Column(Modifier.weight(1f)) {
            Text(label, color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
            if (!open) Text(closedLine, color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }
    }
    if (open) {
        Column(Modifier.fillMaxWidth().padding(start = 4.dp)) { body() }
    }
    Spacer(Modifier.height(6.dp))
}

/**
 * HEALTH: Health Connect (where Samsung Health and the watch write) to the box, every six hours
 * by itself and here on request. The permission state, how the last hand-over went, and the probe
 * that says what Health Connect holds and which app put it there.
 */
@Composable
private fun HealthSection() {
    val hctx = androidx.compose.ui.platform.LocalContext.current
    val scope = androidx.compose.runtime.rememberCoroutineScope()
    var tick by remember { mutableIntStateOf(0) }
    var healthMsg by remember { mutableStateOf("") }
    var grantedCount by remember { mutableStateOf(0) }
    val total = com.localghost.app.sync.HealthSync.PERMISSIONS.size
    val available = remember { com.localghost.app.sync.HealthSync.available(hctx) }
    LaunchedEffect(tick) {
        if (available) grantedCount = com.localghost.app.sync.HealthSync.grantedCount(hctx)
    }
    val granted = grantedCount == total
    val permLauncher = androidx.activity.compose.rememberLauncherForActivityResult(
        androidx.health.connect.client.PermissionController.createRequestPermissionResultContract()) { g ->
        grantedCount = com.localghost.app.sync.HealthSync.PERMISSIONS.count { it in g }
        healthMsg = when {
            grantedCount == total -> "all health permissions granted , the last week ships now"
            grantedCount > 0 -> "$grantedCount of $total granted , what is granted ships, the rest is named as skipped"
            else -> "no permissions granted , health stays off (your call)"
        }
        if (grantedCount > 0) scope.launch { com.localghost.app.sync.HealthSync.sync(hctx); tick++ }
    }
    if (!available) {
        Text("Health Connect is not on this phone , nothing to read", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        return
    }
    val run = remember(tick) { com.localghost.app.sync.HealthSync.lastRun(hctx) }
    Text(when { granted -> "allowed: all $total kinds"; grantedCount > 0 -> "allowed: $grantedCount of $total kinds"; else -> "not allowed yet" },
        color = if (granted) GhostText else Warning, style = MaterialTheme.typography.bodyMedium)
    Text(HealthStatus.line(null, run?.at ?: 0L, run?.days ?: 0, run?.newestDay ?: "", run?.error ?: "", run?.skipped ?: "", System.currentTimeMillis() / 1000)
        .removePrefix("the box holds no day yet · "),
        color = if (run != null && run.error.isNotEmpty()) Warning else GhostTextDim, style = MaterialTheme.typography.labelMedium)
    Spacer(Modifier.height(8.dp))
    if (!granted) {
        GhostButton(if (grantedCount > 0) "ALLOW THE REST" else "ALLOW HEALTH CONNECT", onClick = {
            healthMsg = "opening the Health Connect permission sheet…"
            try { permLauncher.launch(com.localghost.app.sync.HealthSync.PERMISSIONS) }
            catch (e: Exception) { healthMsg = "! permission sheet refused to open: ${e.message ?: "no reason given"}" }
        }, modifier = Modifier.fillMaxWidth())
        Spacer(Modifier.height(8.dp))
    }
    if (grantedCount > 0) {
        Text("[ send the last week now ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.clickable {
                scope.launch {
                    healthMsg = "reading Health Connect…"
                    val res = com.localghost.app.sync.HealthSync.sync(hctx)
                    val skip = if (res.skipped.isEmpty()) "" else " (skipped: ${res.skipped.joinToString(", ")})"
                    healthMsg = when {
                        res.error != null -> "! ${res.error}$skip"
                        res.days > 0 -> "shipped ${res.days} day(s) to your box$skip"
                        else -> "no health data found for the last 7 days$skip , tap [ what is in Health Connect? ]"
                    }
                    tick++
                }
            }.padding(vertical = 6.dp))
        Text("[ send the whole history ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.clickable {
                scope.launch {
                    healthMsg = "walking your history month by month…"
                    val res = com.localghost.app.sync.HealthSync.syncAll(hctx) { p -> healthMsg = p }
                    val skip = if (res.skipped.isEmpty()) "" else " (skipped: ${res.skipped.joinToString(", ")})"
                    healthMsg = when {
                        res.error != null -> "! ${res.error}$skip"
                        res.days > 0 -> "done , ${res.days} day(s) of history on your box$skip"
                        else -> "no health history found$skip"
                    }
                    tick++
                }
            }.padding(vertical = 6.dp))
    }
    var probeLines by remember { mutableStateOf<List<String>>(emptyList()) }
    var probing by remember { mutableStateOf(false) }
    Text(if (probing) "[ reading Health Connect… ]" else "[ what is in Health Connect? ]",
        color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
        modifier = Modifier.clickable {
            if (!probing) {
                probing = true
                scope.launch { probeLines = com.localghost.app.sync.HealthSync.probe(hctx); probing = false }
            }
        }.padding(vertical = 6.dp))
    probeLines.forEach { l -> Text("  $l", color = TerminalDim, style = MaterialTheme.typography.labelMedium) }
    if (probeLines.isNotEmpty()) Text("  a type with nothing in it is not shared with Health Connect: Samsung Health › Settings › Health Connect › allow it",
        color = TerminalDim, style = MaterialTheme.typography.labelMedium)
    if (healthMsg.isNotEmpty()) {
        Spacer(Modifier.height(6.dp))
        Text(healthMsg, color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
    }
}

@Composable
private fun toggleRow(label: String, sub: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(label, color = GhostText, style = MaterialTheme.typography.bodyMedium)
            Text(sub, color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }
        Switch(
            checked = checked, onCheckedChange = onChange,
            colors = SwitchDefaults.colors(
                checkedThumbColor = Void, checkedTrackColor = TerminalGreen,
                uncheckedThumbColor = GhostTextDim, uncheckedTrackColor = VoidLighter,
                uncheckedBorderColor = GhostBorder,
            ),
        )
    }
}

@Composable
private fun WipeButton(onWipe: () -> Unit) {
    var confirming by remember { mutableStateOf(false) }
    androidx.compose.material3.OutlinedButton(
        onClick = { confirming = true },
        shape = androidx.compose.ui.graphics.RectangleShape,
        border = androidx.compose.foundation.BorderStroke(1.dp, Warning),
        colors = androidx.compose.material3.ButtonDefaults.outlinedButtonColors(contentColor = Warning),
        modifier = Modifier.fillMaxWidth(),
    ) { Text("[ FORGET THE BOX ON THIS PHONE ]", style = MaterialTheme.typography.labelLarge) }

    if (confirming) {
        ConfirmDialog(
            title = "WIPE THIS PHONE",
            body = "This destroys the box connection, the device certificate, and the identity key on " +
                "this phone. The box keeps your data. To use this phone again you re-pair it, which " +
                "needs to be done at home with your security key. There is no undo from here.",
            requireWord = "WIPE",
            confirmLabel = "FORGET THE BOX",
            onConfirm = { confirming = false; onWipe() },
            onDismiss = { confirming = false },
        )
    }
}

/**
 * SERVER: the build the box runs, a newer release from the mirror (the phone checks once a day,
 * update/ServerUpdates.kt), and DEPLOY. The box checks the release's signature with the key it
 * already holds, puts it on, locks and restarts onto it, so the app locks too; unlock again when it
 * is back. On trial until its first unlock has run ten minutes with the daemons up; back by itself
 * if it fails, or with ROLL BACK.
 */
@Composable
private fun ServerUpdateSection(onLock: () -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val scope = androidx.compose.runtime.rememberCoroutineScope()
    var status by remember { mutableStateOf<com.localghost.app.net.BoxClient.UpdateStatus?>(null) }
    var offer by remember { mutableStateOf(com.localghost.app.update.ServerUpdates.lastOffer(ctx)) }
    var busy by remember { mutableStateOf("") }
    var result by remember { mutableStateOf("") }
    LaunchedEffect(Unit) {
        status = com.localghost.app.net.BoxClient.updateStatus(ctx)
        status?.let { com.localghost.app.update.ServerUpdates.noteBoxVersion(ctx, it.version) }
        if (offer == null) offer = com.localghost.app.update.ServerUpdates.check(ctx)
    }
    SectionLabel("SERVER")
    Spacer(Modifier.height(8.dp))
    val st = status
    Text(when {
        st == null -> "the box has not said which build it runs (an older build, or it is out of reach)"
        else -> "your box runs ${st.version}" + when (st.trialState) {
            "trial" -> " · on trial: back to ${st.trialPrev} by itself if its first unlock fails; confirmed after ten minutes up"
            "rolled_back" -> " · ${st.trialVersion} was rolled back: ${st.trialReason}"
            else -> ""
        }
    }, color = GhostText, style = MaterialTheme.typography.bodyMedium)
    val o = offer
    val newer = o != null && st != null && com.localghost.app.update.ReleaseInfo.newer(o.release.version, st.version)
    if (o != null && newer) {
        Spacer(Modifier.height(6.dp))
        Text("${o.release.version} is out" + (if (o.release.date.isNotEmpty()) " (${o.release.date.take(10)})" else "") +
            ", ${o.release.changes.size} change${if (o.release.changes.size == 1) "" else "s"}" +
            (if (o.release.since.isNotEmpty()) " since ${o.release.since}" else "") + ":",
            color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
        o.release.changes.take(12).forEach {
            Text("  $it", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }
        if (o.release.changes.size > 12) Text("  … ${o.release.changes.size - 12} more", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
    } else if (o != null && st != null) {
        Spacer(Modifier.height(4.dp))
        Text("the newest release on the mirror is ${o.release.version}: nothing to deploy", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
    }
    if (busy.isNotEmpty()) {
        Spacer(Modifier.height(6.dp))
        Text("> $busy", color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
    }
    if (result.isNotEmpty()) {
        Spacer(Modifier.height(6.dp))
        Text(result, color = GhostText, style = MaterialTheme.typography.labelMedium)
    }
    Spacer(Modifier.height(8.dp))
    if (o != null && newer && busy.isEmpty()) {
        GhostButton("DEPLOY ${o.release.version}", {
            busy = "starting…"; result = ""
            scope.launch {
                val (ok, what) = com.localghost.app.update.ServerUpdates.deploy(ctx, o) { busy = it }
                busy = ""
                if (ok) {
                    result = "your box is restarting onto $what. It locks as it does: unlock it again in a minute."
                    kotlinx.coroutines.delay(2500)
                    onLock()
                } else result = "not deployed: $what"
            }
        }, modifier = Modifier.fillMaxWidth())
    }
    if (st != null && (st.trialState == "trial" || st.trialState == "confirmed") && busy.isEmpty()) {
        Spacer(Modifier.height(8.dp))
        GhostButton("ROLL BACK TO ${st.trialPrev.ifEmpty { "THE EARLIER BUILD" }}", {
            busy = "putting the earlier build back…"; result = ""
            scope.launch {
                val (ok, why) = com.localghost.app.net.BoxClient.updateRollback(ctx)
                busy = ""
                if (ok) { result = "your box is restarting onto the earlier build. Unlock it again in a minute."; kotlinx.coroutines.delay(2500); onLock() }
                else result = "not rolled back: $why"
            }
        }, modifier = Modifier.fillMaxWidth())
    }
    Text("[ check the mirror now ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
        modifier = Modifier.clickable {
            scope.launch {
                busy = "reading the mirror…"; result = ""
                val fresh = com.localghost.app.update.ServerUpdates.check(ctx)
                offer = fresh ?: offer
                status = com.localghost.app.net.BoxClient.updateStatus(ctx) ?: status
                busy = ""
                if (fresh == null) result = "no server release on the mirror yet, or the mirror did not answer"
            }
        }.padding(vertical = 6.dp))
}
