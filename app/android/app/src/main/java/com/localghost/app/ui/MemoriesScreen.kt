package com.localghost.app.ui

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.graphics.asImageBitmap
import android.Manifest
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.ui.Alignment
import androidx.compose.ui.platform.LocalView
import androidx.core.content.ContextCompat
import com.localghost.app.checkin.Feelings
import com.localghost.app.net.BoxClient
import com.localghost.app.net.LifeContext
import com.localghost.app.ui.theme.*
import com.localghost.app.voice.VoiceCapture
import com.localghost.app.voice.VoiceNotes
import com.localghost.app.voice.VoicePlayback
import kotlinx.coroutines.launch

/**
 * MEMORIES , the real corpus off /v1/memories, SOVEREIGN in both directions: everything here is
 * viewable, editable, addable, and deletable by the person, and the model may never overwrite an
 * edit or resurrect a deletion. ghost.synthd writes the distilled rows (from chats now; from the
 * journal entries framed/voiced/noted/tallyd write, as those land); rows the person authors are
 * kind=user and untouchable from birth.
 */
@Composable
fun MemoriesScreen(context: LifeContext?, open: String = "", onOpened: () -> Unit = {}, onOpenDay: (String) -> Unit = {}) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var rows by remember { mutableStateOf<List<BoxClient.MemRow>?>(null) }
    var adding by remember { mutableStateOf(false) }
    var memQuery by remember { mutableStateOf("") }
    var memKind by remember { mutableStateOf("all") }
    var otd by remember { mutableStateOf<List<BoxClient.OtdYear>?>(null) }
    var otdOpen by remember { mutableStateOf(false) }
    var otdLoading by remember { mutableStateOf(false) }
    var checkinHist by remember { mutableStateOf<List<BoxClient.CheckinRow>>(emptyList()) }
    var histOpen by remember { mutableStateOf(false) }
    // VOICE NOTES , the box's list (with transcripts) and what still waits on the phone
    var voiceOpen by remember { mutableStateOf(false) }
    var voiceList by remember { mutableStateOf<List<BoxClient.VoiceNoteRow>?>(null) }
    var voiceLocal by remember { mutableStateOf<List<VoiceNotes.Pending>>(emptyList()) }
    fun reloadVoice() {
        voiceLocal = VoiceNotes.pending(ctx)
        scope.launch { voiceList = BoxClient.voiceNotes(ctx) }
    }
    fun reloadCheckins() {
        voiceLocal = VoiceNotes.pending(ctx)
        scope.launch { checkinHist = BoxClient.checkins(ctx) ?: emptyList() }
        if (voiceOpen) reloadVoice()
    }
    LaunchedEffect(Unit) {
        // what waits on the phone goes first, so the list below shows it on the box
        VoiceNotes.uploadPending(ctx)
        voiceLocal = VoiceNotes.pending(ctx)
        checkinHist = BoxClient.checkins(ctx) ?: emptyList()
    }
    var jotting by remember { mutableStateOf(false) }
    var jotSent by remember { mutableStateOf(false) }
    // WHAT YOU PHOTOGRAPH and NEAR YOU , the taste synthd distils from the photos' tags, and the
    // places around the phone's last fix that fit it. Both load on tap, from the box, never the net.
    var tasteOpen by remember { mutableStateOf(false) }
    var taste by remember { mutableStateOf<BoxClient.Taste?>(null) }
    var tasteLoading by remember { mutableStateOf(false) }
    var nearOpen by remember { mutableStateOf(false) }
    var near by remember { mutableStateOf<BoxClient.Nearby?>(null) }
    var nearLoading by remember { mutableStateOf(false) }
    var nearKm by remember { mutableStateOf(15) }
    // WHERE "NEAR" IS: this phone's newest fix it can read (the sealed last point, or the recent
    // ring while unlocked), else the box's newest trail point. It used to be the phone's last
    // point alone, and when that could not be read it said "no position yet" to a person whose
    // trail was on and on the box.
    var nearFix by remember { mutableStateOf(com.localghost.app.sync.LocationLog.newest(ctx)) }
    var nearFromBox by remember { mutableStateOf(false) }
    var nearLooked by remember { mutableStateOf(false) }
    val trailActive = remember { com.localghost.app.sync.LocationLog.active(ctx) }
    fun loadNear() {
        nearLoading = true
        scope.launch {
            if (nearFix == null) {
                // the box's newest point: the last vertex of the newest day it has a track for
                val t = BoxClient.geoDayTracks(ctx, 2)?.filter { it.n >= 1 && it.times.size == it.n }?.maxByOrNull { it.times.last() }
                if (t != null) {
                    nearFix = com.localghost.app.sync.LocationLog.Point(t.times.last(), t.lat.last(), t.lon.last())
                    nearFromBox = true
                }
                nearLooked = true
            }
            val fix = nearFix
            near = if (fix != null) BoxClient.nearby(ctx, fix.lat, fix.lon, nearKm) else null
            nearLoading = false
        }
    }
    fun reload() { scope.launch { rows = BoxClient.memoriesList(ctx) } }
    LaunchedEffect(Unit) { reload() }
    // what a notification opened: "near" opens NEAR YOU; a memory's id filters the list to it
    LaunchedEffect(open, rows) {
        when {
            open.isEmpty() -> return@LaunchedEffect
            open == "near" -> {
                nearOpen = true
                if (near == null && !nearLoading) loadNear()
                onOpened()
            }
            else -> {
                val list = rows ?: return@LaunchedEffect
                val id = open.toLongOrNull()
                list.firstOrNull { it.id == id }?.let { memQuery = it.title }
                onOpened()
            }
        }
    }

    LazyColumn(Modifier.fillMaxSize().padding(horizontal = 20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item {
            Spacer(Modifier.height(12.dp))
            SectionLabel("MEMORIES")
            Spacer(Modifier.height(6.dp))
            if (context != null) {
                Text("indexed on the box · never leaves it", color = GhostTextDim,
                    style = MaterialTheme.typography.labelMedium)
            }
            Spacer(Modifier.height(4.dp))
            Row {
                Text(if (adding) "[ − cancel ]" else "[ + add a memory ]", color = TerminalGreen,
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { adding = !adding; jotting = false })
                Spacer(Modifier.width(14.dp))
                // A JOT goes to the JOURNAL, not straight to memories: noted ingests it, synthd
                // decides at distillation whether it is durable , same path as a shared email.
                Text(if (jotting) "[ − cancel ]" else "[ + jot a note ]", color = TerminalGreen,
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { jotting = !jotting; adding = false })
            }
            if (jotSent) Text("sent to the journal , distilled within minutes", color = TerminalDim,
                style = MaterialTheme.typography.labelMedium)
        }
        item {
            // ABOUT ME AND MY PEOPLE: a note the box makes memories from (one per person, and facts
            // about me), and the chat starts every question from
            AboutCard(onSaved = { reload() })
        }
        item {
            CheckinCard(history = checkinHist, onSaved = { reloadCheckins() })
        }
        if (checkinHist.isNotEmpty()) item {
            Text(if (histOpen) "[ − past check-ins ]" else "[ + past check-ins (${checkinHist.size}) ]",
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { histOpen = !histOpen })
            if (histOpen) Column(Modifier.animateContentSize()) {
                val onPhone = voiceLocal.map { it.id }.toSet()
                checkinHist.take(14).forEach { r ->
                    Text("${r.day} · ${r.feelings}", color = GhostTextDim,
                        style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.padding(top = 2.dp))
                    r.voice?.let { v -> key("ci-" + v.id) { VoiceNoteCard(v, onPhone = v.id in onPhone, onDelete = null) } }
                }
            }
        }
        item {
            // VOICE NOTES , everything said, newest first: what still waits on the phone, then the
            // box's notes with their transcripts. Deleting one removes the audio, the words and the
            // journal entry on the box.
            val count = (voiceList?.size ?: 0) + voiceLocal.size
            Text(if (voiceOpen) "[ − voice notes ]" else "[ + voice notes" + (if (count > 0) " ($count)" else "") + " , what you said, transcribed on your box ]",
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    voiceOpen = !voiceOpen
                    if (voiceOpen) reloadVoice()
                })
            if (voiceOpen) Column(Modifier.animateContentSize()) {
                voiceLocal.forEach { p ->
                    key("local-" + p.id) {
                        VoiceNoteCard(BoxClient.VoiceNoteRow(p.id, p.kind, p.day, p.takenAt / 1000, p.durationMs, "missing", "", "", ""),
                            onPhone = true, onDelete = { VoiceNotes.deleteLocal(ctx, p.id); reloadVoice() })
                    }
                }
                val list = voiceList
                when {
                    list == null -> Text("asking the box…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    list.isEmpty() && voiceLocal.isEmpty() -> Text("! none yet , record one at the check-in above",
                        color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    else -> list.forEach { v ->
                        key("box-" + v.id) {
                            VoiceNoteCard(v, onPhone = false, onDelete = {
                                scope.launch { BoxClient.voiceDelete(ctx, v.id); reloadCheckins(); reloadVoice() }
                            })
                        }
                    }
                }
            }
        }
        item {
            // WHAT YOU PHOTOGRAPH , the taste. Tags ranked by the share of photo days they appear on,
            // so a burst of five hundred beach photos on one day counts as one day; then the interests
            // (beaches, harbours, peaks ...) those tags add up to. Assembled on the box from the tags,
            // no model call, refreshed as the pipeline tags more.
            Text(if (tasteOpen) "[ − what you photograph ]" else "[ + what you photograph , what the archive says you like ]",
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    tasteOpen = !tasteOpen
                    if (tasteOpen && taste == null && !tasteLoading) {
                        tasteLoading = true
                        scope.launch { taste = BoxClient.taste(ctx); tasteLoading = false }
                    }
                })
            if (tasteOpen) {
                val t = taste
                when {
                    tasteLoading -> Text("reading the tags…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    t == null -> Text("! the box did not answer", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    t.likes.isEmpty() -> {
                        val msg = if (t.note.isNotBlank()) t.note else if (t.summary.isNotBlank()) t.summary else "nothing tagged yet"
                        Text("! $msg", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    }
                    else -> TasteCard(t)
                }
            }
        }
        item {
            // NEAR YOU , the taste laid over the box's own map data around the phone's last fix:
            // "a beach 2 km north-east you have never photographed". The position goes to the box,
            // which answers from its own tables; nothing leaves it.
            Text(if (nearOpen) "[ − near you ]" else "[ + near you , places that fit, from your own map data ]",
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    nearOpen = !nearOpen
                    if (nearOpen && near == null && !nearLoading) loadNear()
                })
            if (nearOpen) {
                val n = near
                val fix = nearFix
                when {
                    nearLoading -> Text("asking the box what is within $nearKm km…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    fix == null && !trailActive -> Text("! no position , turn on the location trail (settings) and the box can say what is around you",
                        color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    fix == null && nearLooked -> Text("! the trail is on, but neither this phone nor the box has a point to measure from yet , SETTINGS › LOCATION TRAIL says where it is",
                        color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    fix == null -> Text("! no position yet", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    n == null -> Text("! the box did not answer", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    else -> {
                        Text(TrailStatus.nearFrom(nearFromBox, fix.ts, System.currentTimeMillis() / 1000),
                            color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                        NearbyCard(n, nearKm, onKm = { k -> nearKm = k; near = null; loadNear() })
                    }
                }
            }
        }
        item {
            // ON THIS DAY , synthd's retrospective. Loaded on TAP, not on entry: the first build
            // of a day narrates through the model and can take a minute; cached days are instant.
            Text(if (otdOpen) "[ − on this day ]" else "[ + on this day , what were you doing? ]",
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    otdOpen = !otdOpen
                    if (otdOpen && otd == null && !otdLoading) {
                        otdLoading = true
                        scope.launch { otd = BoxClient.onThisDay(ctx); otdLoading = false }
                    }
                })
            if (otdOpen) {
                when {
                    otdLoading -> Text("composing from your history… (first time today takes a minute)",
                        color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    otd?.isEmpty() != false -> if (!otdLoading && otd != null)
                        Text("! no history for this date yet , it grows as years of photos and notes accumulate",
                            color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    else -> {}
                }
            }
        }
        if (otdOpen && otd?.isNotEmpty() == true) items(otd!!, key = { "otd-${it.year}" }) { y ->
            OtdYearCard(y) { onOpenDay(DayText.shiftYears(DayText.of(System.currentTimeMillis() / 1000), -y.yearsAgo)) }
        }
        if (jotting) item {
            MemoryEditor(initTitle = "", initBody = "", onSave = { t, b ->
                scope.launch {
                    jotSent = BoxClient.noteAdd(ctx, (t + "\n\n" + b).trim())
                    jotting = false
                }
            }, onCancel = { jotting = false })
        }
        if (adding) item {
            MemoryEditor(initTitle = "", initBody = "", onSave = { t, b ->
                scope.launch { BoxClient.memoryAdd(ctx, t, b); adding = false; reload() }
            }, onCancel = { adding = false })
        }
        when {
            rows == null -> item {
                Text("reading memories from the box…", color = GhostTextDim,
                    style = MaterialTheme.typography.bodyMedium)
            }
            rows!!.isEmpty() -> item {
                Text("! nothing distilled yet , finished conversations become memories within " +
                     "minutes; photos join as the journal fills", color = TerminalDim,
                    style = MaterialTheme.typography.bodyMedium)
            }
            else -> {
                item {
                    BasicTextField(memQuery, { memQuery = it }, singleLine = true,
                        textStyle = MaterialTheme.typography.bodySmall.copy(color = GhostText),
                        cursorBrush = SolidColor(TerminalGreen),
                        decorationBox = { inner -> Box(Modifier.fillMaxWidth()
                            .border(1.dp, GhostBorder, RectangleShape).padding(8.dp)) {
                            if (memQuery.isEmpty()) Text("filter memories…", color = TerminalDim,
                                style = MaterialTheme.typography.bodySmall); inner() } },
                        modifier = Modifier.fillMaxWidth())
                    Spacer(Modifier.height(6.dp))
                    val yours = rows!!.count { it.kind == "user" }
                    Text("${rows!!.size} memories" + (if (yours > 0) " · $yours yours" else ""),
                        color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                    Spacer(Modifier.height(6.dp))
                    // the kinds, as chips: one picked shows only its memories
                    val counts = MemoryKinds.counts(rows!!.map { it.kind })
                    Row(Modifier.horizontalScroll(rememberScrollState())) {
                        MemoryKinds.all.forEach { k ->
                            val n = counts[k.id] ?: 0
                            if (k.id != "all" && n == 0) return@forEach
                            val on = memKind == k.id
                            Text(k.label + (if (k.id != "all") " $n" else ""), color = if (on) Void else TerminalGreen,
                                style = MaterialTheme.typography.labelMedium,
                                modifier = Modifier.padding(end = 6.dp).border(1.dp, if (on) TerminalGreen else TerminalDim, RectangleShape)
                                    .background(if (on) TerminalGreen else Void).clickable { memKind = k.id }
                                    .padding(horizontal = 10.dp, vertical = 4.dp))
                        }
                    }
                }
                items(rows!!.filter { MemoryKinds.matches(memKind, it.kind) && (memQuery.isBlank() ||
                        it.title.contains(memQuery, true) || it.body.contains(memQuery, true)) },
                    key = { "mem-${it.id}" }) { m ->
                MemoryRowCard(m,
                    onEdit = { t, b -> scope.launch { BoxClient.memoryEdit(ctx, m.id, t, b); reload() } },
                    onDelete = { scope.launch { BoxClient.memoryDelete(ctx, m.id); reload() } })
                }
            }
        }
        item { Spacer(Modifier.height(20.dp)) }
    }
}

/** The DAILY CHECK-IN , "how are you feeling today, and why". Feelings as chips: a quick row (the
 *  box's guesses from the day's shape, then your usual ones) with the full list behind "more"; the
 *  box's first two guesses are ticked before you look and marked "·" as its guess. A why prefilled
 *  from what the box knows about today, and a VOICE NOTE: say the day in your own words, and the
 *  box transcribes it (whisper.cpp, on the box) into the journal beside the check-in. It all lands
 *  in the JOURNAL via /v1/notes and /v1/voice , synthd distils it like everything else. Once a day:
 *  afterwards the card is a checkmark and a recorder for more notes to the same day. */
@Composable
private fun CheckinCard(history: List<BoxClient.CheckinRow>, onSaved: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val today = remember { java.text.SimpleDateFormat("yyyy-MM-dd", java.util.Locale.US).format(java.util.Date()) }
    var done by remember { mutableStateOf(com.localghost.app.settings.AppSettings.lastCheckinDay(ctx) == today) }
    var justSaved by remember { mutableStateOf(false) } // this open saved it: write the day up now
    var open by remember { mutableStateOf(true) }
    var why by remember { mutableStateOf("") }
    var prefilled by remember { mutableStateOf(false) }
    var suggested by remember { mutableStateOf<List<String>>(emptyList()) }
    var preselected by remember { mutableStateOf<List<String>>(emptyList()) }
    var picked by remember { mutableStateOf<List<String>>(emptyList()) }
    var touched by remember { mutableStateOf(false) }
    var more by remember { mutableStateOf(false) }
    var saving by remember { mutableStateOf(false) }
    var note by remember { mutableStateOf("") }
    val usual = remember(history) { Feelings.usual(history.take(30).map { it.feelings }) }
    val quick = remember(suggested, usual) { Feelings.quick(suggested, usual) }
    val rec by VoiceCapture.state.collectAsState()

    // the prefill: the box's guesses (the first two ticked, unless the person already chose) and a
    // why from the day's numbers; asked once, when the card is open
    LaunchedEffect(open) {
        if (!open || prefilled || done) return@LaunchedEffect
        prefilled = true
        val d = BoxClient.daySummary(ctx) ?: return@LaunchedEffect
        suggested = d.suggested.filter { it in Feelings.all }
        if (!touched) {
            preselected = Feelings.preselect(suggested)
            picked = preselected
        }
        if (why.isEmpty()) {
            val bits = ArrayList<String>()
            if (d.sleepMinutes > 0) bits.add("slept ${d.sleepMinutes / 60}h${"%02d".format(d.sleepMinutes % 60)}m")
            if (d.steps > 0) bits.add("${"%,d".format(d.steps)} steps")
            if (d.exerciseMinutes >= 10) bits.add("${d.exerciseMinutes}m exercise")
            if (d.photos > 0) bits.add("${d.photos} photo${if (d.photos == 1) "" else "s"}" +
                (if (d.places.isNotEmpty()) " around " + d.places.take(2)
                    .joinToString(" and ") { it.substringAfterLast(" / ") } else ""))
            if (d.places.isNotEmpty()) bits.add("was at " +
                d.places.take(3).joinToString("; ") { it.substringAfterLast(" / ") })
            d.notes.filterNot { it.startsWith("Voice note") }.take(2).forEach { bits.add(it) }
            why = if (bits.isEmpty()) "" else "Today: " + bits.joinToString(". ") + "."
        }
    }

    if (done) {
        Column(Modifier.fillMaxWidth().animateContentSize()) {
            Text("✓ checked in today", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            Spacer(Modifier.height(4.dp))
            // YOUR DAY, written up: once the check-in is in, the box folds the photos, the
            // trail, the health sync, the voice notes and what you said into one telling of the
            // day (synthd days.go). Written on request right after the check-in (a minute or
            // two: it waits for the check-in to land, then the model writes); read back after.
            DayStoryCard(today, justSaved = justSaved)
            Spacer(Modifier.height(8.dp))
            VoiceRecorder(hint = "say more about today , kept and transcribed on your box",
                saveLabel = "[ save to today's journal ]", onSave = { take ->
                    VoiceNotes.enqueue(ctx, take, "journal", today)
                    VoiceCapture.taken()
                    scope.launch { VoiceNotes.uploadPending(ctx); onSaved() }
                })
        }
        return
    }
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Text(if (open) "[ − how are you feeling today? ]" else "[ + how are you feeling today? ]",
            color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.clickable { open = !open })
        if (open) {
            history.firstOrNull { it.day != today }?.let { y ->
                Spacer(Modifier.height(4.dp))
                Text("${y.day} you felt ${y.feelings}", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            }
            Spacer(Modifier.height(8.dp))
            val tap: (String) -> Unit = { f -> touched = true; picked = Feelings.toggle(picked, f) }
            FeelingRows(quick, picked, suggested, tap)
            if (more) {
                for (g in Feelings.groups) {
                    val rest = g.feelings.filter { it !in quick }
                    if (rest.isEmpty()) continue
                    Spacer(Modifier.height(6.dp))
                    Text("${g.label} , ${g.hint}", color = TerminalDim, style = MaterialTheme.typography.labelSmall)
                    FeelingRows(rest, picked, suggested, tap)
                }
            }
            Spacer(Modifier.height(2.dp))
            Row {
                Text(if (more) "[ − fewer ]" else "[ + more feelings ]", color = TerminalGreen,
                    style = MaterialTheme.typography.labelMedium, modifier = Modifier.clickable { more = !more })
                Spacer(Modifier.width(10.dp))
                Text("${picked.size} of ${Feelings.MAX_PICKS}", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            }
            if (preselected.isNotEmpty()) {
                Text("· = the box's guess from your day (sleep, steps, places); tap to change",
                    color = TerminalDim, style = MaterialTheme.typography.labelSmall)
            }
            Spacer(Modifier.height(6.dp))
            BasicTextField(why, { why = it },
                textStyle = MaterialTheme.typography.bodySmall.copy(color = GhostText),
                cursorBrush = SolidColor(TerminalGreen),
                decorationBox = { inner -> Box(Modifier.fillMaxWidth().border(1.dp, GhostBorder, RectangleShape)
                    .padding(8.dp)) { if (why.isEmpty()) Text("why? (prefilled from your day , edit freely)",
                        color = TerminalDim, style = MaterialTheme.typography.bodySmall); inner() } },
                modifier = Modifier.fillMaxWidth().heightIn(min = 44.dp))
            Spacer(Modifier.height(8.dp))
            // the take in hand goes with the check-in when it is saved
            VoiceRecorder(hint = "or say it: a minute about the day, in your own words , kept and transcribed on your box",
                saveLabel = null, onSave = {})
            Spacer(Modifier.height(8.dp))
            val take = rec.take
            val canSave = !saving && !rec.recording && (picked.isNotEmpty() || why.isNotBlank() || take != null)
            Text(when { saving -> "saving…"; rec.recording -> "[ save check-in ] , stop the recording first"; else -> "[ save check-in ]" },
                color = if (canSave) TerminalGreen else TerminalDim, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    if (!canSave) return@clickable
                    saving = true
                    note = ""
                    scope.launch {
                        val text = Feelings.checkinText(today, picked, preselected, why, take?.id, take?.durationMs ?: 0L)
                        if (BoxClient.noteAdd(ctx, text)) {
                            if (take != null) {
                                VoicePlayback.stop()
                                VoiceNotes.enqueue(ctx, take, "checkin", today)
                                VoiceCapture.taken()
                            }
                            com.localghost.app.settings.AppSettings.setLastCheckinDay(ctx, today)
                            justSaved = true
                            done = true
                            if (take != null) VoiceNotes.uploadPending(ctx)
                            onSaved()
                        } else {
                            note = "! the box did not answer , nothing is lost (the voice note stays here); try again when it is reachable"
                        }
                        saving = false
                    }
                })
            if (note.isNotEmpty()) Text(note, color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        }
    }
}

/** The day, told by the box after the check-in. [justSaved]: ask the box to write it now (the
 *  voice note, if any, is still being transcribed; the story is written again by the nightly pass
 *  once it lands). Otherwise read what is there. */
@Composable
private fun DayStoryCard(day: String, justSaved: Boolean) {
    val ctx = LocalContext.current
    var story by remember { mutableStateOf<BoxClient.DayStory?>(null) }
    var state by remember { mutableStateOf(if (justSaved) "writing" else "reading") }
    var open by remember { mutableStateOf(true) }
    LaunchedEffect(day, justSaved) {
        if (justSaved) {
            // the voice note's upload goes first, so the transcript can be in the telling
            kotlinx.coroutines.delay(2_000)
            val s = BoxClient.dayStory(ctx, day, build = true)
            story = s
            state = if (s == null) "failed" else if (s.summary.isBlank()) "empty" else "ok"
        } else {
            val s = BoxClient.dayStory(ctx, day)
            story = s
            state = if (s == null) "failed" else if (s.summary.isBlank()) "empty" else "ok"
        }
    }
    Column(Modifier.fillMaxWidth().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Text(if (open) "[ − your day, as the box tells it ]" else "[ + your day, as the box tells it ]",
            color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.clickable { open = !open })
        if (open) {
            Spacer(Modifier.height(6.dp))
            when (state) {
                "writing" -> Text("writing up your day from the photos, the trail, the health sync, your voice note and what you just said… a minute or two",
                    color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                "reading" -> Text("reading…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                "failed" -> Text("! the box did not answer , the day is written by the nightly pass anyway; it will be here tomorrow, and in MEMORIES",
                    color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                "empty" -> Text("nothing to tell yet , the day is written once there are photos, a trail or a check-in on the box",
                    color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                else -> story?.let { s ->
                    if (s.title.isNotBlank()) Text(s.title, color = GhostText, style = MaterialTheme.typography.titleMedium)
                    Spacer(Modifier.height(4.dp))
                    Text(s.summary, color = GhostText, style = MaterialTheme.typography.bodyMedium)
                    Spacer(Modifier.height(4.dp))
                    Text(if (s.writtenBy == "model") "written by the box's model from the day's facts · it is in MEMORIES"
                        else "the day's facts, in the box's plain words · the model writes it up in the evening pass",
                        color = TerminalDim, style = MaterialTheme.typography.labelMedium)
                }
            }
        }
    }
}

/** Feeling chips in rows that fit the width. A picked one is bracketed; the box's guess carries "·". */
@Composable
private fun FeelingRows(words: List<String>, picked: List<String>, guessed: List<String>, onTap: (String) -> Unit) {
    for (row in Feelings.rows(words)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            for (f in row) {
                val on = f in picked
                val mark = if (f in guessed) "·" else ""
                Text(if (on) "[$mark$f]" else "$mark$f ",
                    color = if (on) TerminalGreen else GhostTextDim,
                    style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { onTap(f) }.padding(vertical = 3.dp))
            }
        }
    }
}

/** THE RECORDER on the card: record, stop, listen, again, drop. [saveLabel] null = the take is saved
 *  by the card's own button (the check-in); else this row saves it. The screen stays on while it
 *  records (a locked phone gives an app silence from the microphone). */
@Composable
private fun VoiceRecorder(hint: String, saveLabel: String?, onSave: (VoiceCapture.Take) -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val rec by VoiceCapture.state.collectAsState()
    val playing by VoicePlayback.playing.collectAsState()
    var denied by remember { mutableStateOf(false) }
    val view = LocalView.current
    DisposableEffect(rec.recording) {
        view.keepScreenOn = rec.recording
        onDispose { view.keepScreenOn = false }
    }
    val mic = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        denied = !ok
        if (ok) VoiceCapture.start(ctx)
    }
    fun record() {
        VoicePlayback.stop()
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
            VoiceCapture.start(ctx)
        } else {
            mic.launch(Manifest.permission.RECORD_AUDIO)
        }
    }
    val take = rec.take
    Column(Modifier.fillMaxWidth()) {
        when {
            rec.recording -> Row(verticalAlignment = Alignment.CenterVertically) {
                Text("● " + Feelings.clock(rec.elapsedMs), color = Warning, style = MaterialTheme.typography.labelMedium)
                Spacer(Modifier.width(8.dp))
                Box(Modifier.width(72.dp).height(6.dp).background(GhostBorder)) {
                    Box(Modifier.fillMaxHeight().fillMaxWidth(rec.level.coerceIn(0.02f, 1f)).background(TerminalGreen))
                }
                Spacer(Modifier.width(10.dp))
                Text("[ ■ stop ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { VoiceCapture.stop() })
            }
            take != null -> {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text("🎙 voice note ${Feelings.clock(take.durationMs)}", color = GhostText, style = MaterialTheme.typography.labelMedium)
                    Spacer(Modifier.width(10.dp))
                    Text(if (playing == take.id) "[ ■ ]" else "[ ▶ ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { scope.launch { VoicePlayback.toggle(ctx, take.id) } })
                    Spacer(Modifier.width(10.dp))
                    Text("[ ● again ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { record() })
                    Spacer(Modifier.width(10.dp))
                    Text("[ ✕ ]", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { VoicePlayback.stop(); VoiceCapture.discard() })
                }
                if (saveLabel != null) {
                    Text(saveLabel, color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.padding(top = 4.dp).clickable { VoicePlayback.stop(); onSave(take) })
                } else {
                    Text("saved with the check-in", color = TerminalDim, style = MaterialTheme.typography.labelSmall)
                }
            }
            else -> {
                Text("[ ● record a voice note ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { record() })
                Text(hint, color = TerminalDim, style = MaterialTheme.typography.labelSmall)
            }
        }
        if (rec.error.isNotEmpty()) Text("! " + rec.error, color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        if (denied) Text("! no microphone permission , allow it for LocalGhost in the phone's settings to record",
            color = TerminalDim, style = MaterialTheme.typography.labelMedium)
    }
}

/** A note's line under its check-in, or in the notes list: length, then the words or where it is. */
private fun voiceStatus(v: BoxClient.VoiceNoteRow, onPhone: Boolean): String = when (v.status) {
    "done" -> v.transcript.ifBlank { "(nothing the box could hear)" }
    "pending" -> pendingWhy(v.id, BoxClient.voiceQueue)
    "failed" -> "not transcribed: " + v.error.ifBlank { "the speech engine failed" }
    "missing" -> if (onPhone) "on the phone, waiting to reach the box" else "not on the box"
    else -> v.status
}

/** A waiting note's line, with the box's reason when it has one (an older box sends none). */
internal fun pendingWhy(id: String, q: BoxClient.VoiceQueue?): String = when {
    q == null -> "on the box, waiting to be transcribed"
    q.working == id -> "on the box, being transcribed now"
    !q.running -> "on the box, waiting: the box's voice service is not running"
    q.why.isNotBlank() -> "on the box, waiting: " + q.why
    else -> "on the box, in line to be transcribed"
}

/** One voice note: when, how long, what was said (tap to read it all), play, delete. */
@Composable
private fun VoiceNoteCard(v: BoxClient.VoiceNoteRow, onPhone: Boolean, onDelete: (() -> Unit)?) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val playing by VoicePlayback.playing.collectAsState()
    var full by remember { mutableStateOf(false) }
    var confirmDel by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            val whenText = if (v.takenAt > 0) java.text.SimpleDateFormat("EEE d MMM, HH:mm", java.util.Locale.UK)
                .format(java.util.Date(v.takenAt * 1000)) else v.day
            Text("🎙 $whenText · ${Feelings.clock(v.durationMs)}" + (if (v.kind == "checkin") " · check-in" else ""),
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium, modifier = Modifier.weight(1f))
            if (v.status != "missing" || onPhone) {
                Text(if (playing == v.id) " [ ■ ]" else " [ ▶ ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { scope.launch { failed = !VoicePlayback.toggle(ctx, v.id) } })
            }
            if (onDelete != null) {
                if (confirmDel) {
                    LaunchedEffect(confirmDel) { kotlinx.coroutines.delay(3000); confirmDel = false }
                    Text(" [ delete? ]", color = Warning, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { VoicePlayback.stop(); onDelete() })
                } else {
                    Text(" 🗑", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { confirmDel = true })
                }
            }
        }
        val words = voiceStatus(v, onPhone)
        Text(words, color = if (v.status == "done") GhostText else TerminalDim, style = MaterialTheme.typography.bodySmall,
            maxLines = if (full) Int.MAX_VALUE else 3,
            modifier = Modifier.clickable { full = !full })
        if (failed) Text("! the audio could not be had from the box", color = TerminalDim, style = MaterialTheme.typography.labelSmall)
    }
}

@Composable
private fun OtdYearCard(y: BoxClient.OtdYear, onOpenDay: () -> Unit = {}) {
    val ctx = LocalContext.current
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("${y.year} , ${if (y.yearsAgo == 1) "1 year" else "${y.yearsAgo} years"} ago",
                color = TerminalGreen, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            // the whole day: its story, photos, outing, notes
            Text("the day ›", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { onOpenDay() }.padding(4.dp))
        }
        // the day's title as the box built it (weekday, date, the places), then its route in a line
        if (y.title.isNotBlank()) {
            Text(y.title, color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        }
        if (y.line.isNotBlank()) {
            Text(y.line, color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        }
        if (y.narrative.isNotBlank()) {
            Spacer(Modifier.height(4.dp))
            Text(y.narrative, color = GhostText, style = MaterialTheme.typography.bodySmall)
        }
        if (y.places.isNotEmpty()) {
            Spacer(Modifier.height(4.dp))
            Text("⌖ " + y.places.joinToString(" · "), color = TerminalDim,
                style = MaterialTheme.typography.labelMedium)
        }
        if (y.photos.isNotEmpty()) {
            Spacer(Modifier.height(8.dp))
            // a tap opens the day's photos as a slideshow, from the one tapped
            ThumbStrip(y.photos, title = "${y.year}")
        }
        if (y.notes.isNotEmpty()) {
            Spacer(Modifier.height(6.dp))
            y.notes.take(3).forEach { n ->
                Text("· $n", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}

@Composable
private fun MemoryRowCard(m: BoxClient.MemRow, onEdit: (String, String) -> Unit, onDelete: () -> Unit) {
    var editing by remember { mutableStateOf(false) }
    var confirmDel by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        if (editing) {
            MemoryEditor(initTitle = m.title, initBody = m.body,
                onSave = { t, b -> editing = false; onEdit(t, b) },
                onCancel = { editing = false })
        } else {
            Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                Text(m.title, color = GhostText, style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.weight(1f))
                Text("✎", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { editing = true }.padding(start = 6.dp))
                if (confirmDel) {
                    LaunchedEffect(confirmDel) { kotlinx.coroutines.delay(3000); confirmDel = false }
                    Text(" [ delete? ]", color = Warning, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { onDelete() })
                } else {
                    Text(" 🗑", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { confirmDel = true })
                }
            }
            // AN OUTING (or a DAY) carries its photos: the cover frames synthd picked, spread across it.
            if ((m.kind == "outing" || m.kind == "day") && m.covers.isNotEmpty()) {
                Spacer(Modifier.height(8.dp))
                CoverStrip(m.covers)
            }
            if (m.body.isNotBlank()) {
                Spacer(Modifier.height(4.dp))
                Text(m.body, color = GhostTextDim, style = MaterialTheme.typography.bodySmall)
            }
            Spacer(Modifier.height(4.dp))
            val line = m.outingLine
            val origin = when (m.kind) {
                "user" -> "yours"
                "me" -> "about me, from my note"
                "person" -> "one of my people"
                "place" -> "a place, counted from your trail and photos"
                "insight" -> "noticed by your box"
                "outing" -> if (line != null) "from your photos · $line" else "from your photos"
                "day" -> m.meta?.optString("line")?.takeIf { it.isNotBlank() }?.let { "a day, from your trail and photos · $it" } ?: "a day, from your trail and photos"
                "episode" -> "a day"
                else -> "distilled"
            }
            Text(origin + " · " +
                java.text.SimpleDateFormat("MMM d, yyyy", java.util.Locale.US)
                    .format(java.util.Date(m.createdAt)),
                color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        }
    }
}

@Composable
private fun MemoryEditor(initTitle: String, initBody: String, onSave: (String, String) -> Unit, onCancel: () -> Unit) {
    var title by remember { mutableStateOf(initTitle) }
    var body by remember { mutableStateOf(initBody) }
    Column(Modifier.fillMaxWidth().border(1.dp, TerminalGreen, RectangleShape).padding(10.dp)) {
        BasicTextField(title, { title = it }, singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = TerminalGreen),
            cursorBrush = SolidColor(TerminalGreen),
            decorationBox = { inner -> Box { if (title.isEmpty()) Text("title", color = TerminalDim); inner() } },
            modifier = Modifier.fillMaxWidth())
        Spacer(Modifier.height(6.dp))
        BasicTextField(body, { body = it },
            textStyle = MaterialTheme.typography.bodySmall.copy(color = GhostText),
            cursorBrush = SolidColor(TerminalGreen),
            decorationBox = { inner -> Box { if (body.isEmpty()) Text("what to remember", color = TerminalDim); inner() } },
            modifier = Modifier.fillMaxWidth().heightIn(min = 40.dp))
        Spacer(Modifier.height(6.dp))
        Row {
            Text("[ save ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { if (title.isNotBlank()) onSave(title.trim(), body.trim()) })
            Spacer(Modifier.width(12.dp))
            Text("[ cancel ]", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { onCancel() })
        }
    }
}

/** The cover frames of an outing, thumbnails off /v1/frames/thumb, loaded as they scroll in; a
 *  tap opens them as a slideshow (a video plays). */
@Composable
private fun CoverStrip(hashes: List<String>) {
    ThumbStrip(hashes)
}

/** The taste: one sentence, then the likes by category with their share of photo days, then the
 *  interests the box will match places against. */
@Composable
private fun TasteCard(t: BoxClient.Taste) {
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Text(t.summary, color = GhostText, style = MaterialTheme.typography.bodySmall)
        Spacer(Modifier.height(6.dp))
        Text("over ${t.days} days with a camera out · ${t.photos} photos", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        Spacer(Modifier.height(6.dp))
        val order = listOf("place", "nature", "activity", "food", "animal", "vehicle", "object", "people", "event", "")
        val byCat: Map<String, List<BoxClient.Like>> = t.likes.groupBy { it.category }
        val cats = ArrayList<String>()
        for (c in order) if (byCat.containsKey(c)) cats.add(c)
        for (c in byCat.keys) if (c !in cats) cats.add(c)
        for (cat in cats) {
            val ls = byCat[cat] ?: continue
            Text((cat.ifBlank { "other" }) + ": " + ls.joinToString(" · ") { "${it.tag} ${(it.share * 100).toInt()}%" },
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(vertical = 1.dp))
        }
        if (t.interests.isNotEmpty()) {
            Spacer(Modifier.height(6.dp))
            Text("places to your taste: " + t.interests.take(6).joinToString(" · ") { it.name },
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
        }
    }
}

/** What is around the last fix and fits: name, kind, distance and bearing, then why the box
 *  thinks so and whether you have photographed there before. */
@Composable
private fun NearbyCard(n: BoxClient.Nearby, km: Int, onKm: (Int) -> Unit) {
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Row {
            for (k in listOf(5, 15, 40)) {
                val on = k == km
                Text("$k km", color = if (on) Void else TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.padding(end = 8.dp).border(1.dp, TerminalGreen, RectangleShape)
                        .background(if (on) TerminalGreen else Void).clickable { if (!on) onKm(k) }
                        .padding(horizontal = 10.dp, vertical = 4.dp))
            }
        }
        Spacer(Modifier.height(8.dp))
        if (n.suggestions.isEmpty()) {
            val msg = if (n.note.isNotBlank()) n.note else "nothing within $km km that fits what you photograph"
            Text("! $msg", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        }
        n.suggestions.forEach { s ->
            Column(Modifier.padding(vertical = 4.dp)) {
                Text("${s.name} · ${s.kind} · ${if (s.distanceKm < 1) "${(s.distanceKm * 1000).toInt()} m" else "%.1f km".format(java.util.Locale.US, s.distanceKm)} ${s.bearing}" +
                    (if (s.beenThere == 0) "  · new" else ""),
                    color = if (s.beenThere == 0) GhostText else GhostTextDim, style = MaterialTheme.typography.bodySmall)
                Text(s.why, color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}


/** The note about me and my people: written here, kept on the box, made into memories there. */
@Composable
private fun AboutCard(onSaved: () -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    var open by remember { mutableStateOf(false) }
    var about by remember { mutableStateOf<BoxClient.About?>(null) }
    var text by remember { mutableStateOf("") }
    var saving by remember { mutableStateOf(false) }
    LaunchedEffect(open) {
        if (open) BoxClient.about(ctx)?.let { about = it; text = it.text }
    }
    Text(if (open) "[ − about me and my people ]" else "[ + about me and my people ]", color = TerminalGreen,
        style = MaterialTheme.typography.labelMedium, modifier = Modifier.clickable { open = !open })
    if (!open) return
    Column(Modifier.fillMaxWidth().padding(top = 6.dp).border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Text("who I am, and the people in my life: names, who they are to me, what matters. The box makes memories from it, one per person, and every chat starts from it.",
            color = GhostTextDim, style = MaterialTheme.typography.labelSmall)
        Spacer(Modifier.height(8.dp))
        BasicTextField(text, { if (it.length <= 8000) text = it },
            textStyle = MaterialTheme.typography.bodySmall.copy(color = GhostText),
            cursorBrush = SolidColor(TerminalGreen),
            decorationBox = { inner -> Box(Modifier.fillMaxWidth().heightIn(min = 120.dp)
                .border(1.dp, GhostBorder, RectangleShape).padding(8.dp)) {
                if (text.isEmpty()) Text("I'm … I live in … My partner … My friends …", color = TerminalDim,
                    style = MaterialTheme.typography.bodySmall); inner() } },
            modifier = Modifier.fillMaxWidth())
        Spacer(Modifier.height(6.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(if (saving) "saving…" else "[ save ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable(enabled = !saving && text != about?.text) {
                    saving = true
                    scope.launch {
                        BoxClient.saveAbout(ctx, text.trim())?.let { about = it }
                        saving = false
                        onSaved()
                    }
                })
            Spacer(Modifier.width(12.dp))
            val a = about
            if (a != null) Text(AboutText.status(a.name, a.me, a.people, a.pending, a.text.isNotBlank()),
                color = TerminalDim, style = MaterialTheme.typography.labelSmall)
        }
    }
}
