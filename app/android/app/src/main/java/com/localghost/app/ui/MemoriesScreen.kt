package com.localghost.app.ui

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
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
import com.localghost.app.net.BoxClient
import com.localghost.app.net.LifeContext
import com.localghost.app.ui.theme.*
import kotlinx.coroutines.launch

/**
 * MEMORIES , the real corpus off /v1/memories, SOVEREIGN in both directions: everything here is
 * viewable, editable, addable, and deletable by the person, and the model may never overwrite an
 * edit or resurrect a deletion. ghost.synthd writes the distilled rows (from chats now; from the
 * journal entries framed/voiced/noted/tallyd write, as those land); rows the person authors are
 * kind=user and untouchable from birth.
 */
@Composable
fun MemoriesScreen(context: LifeContext?) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var rows by remember { mutableStateOf<List<BoxClient.MemRow>?>(null) }
    var adding by remember { mutableStateOf(false) }
    var memQuery by remember { mutableStateOf("") }
    var otd by remember { mutableStateOf<List<BoxClient.OtdYear>?>(null) }
    var otdOpen by remember { mutableStateOf(false) }
    var otdLoading by remember { mutableStateOf(false) }
    var checkinHist by remember { mutableStateOf<List<BoxClient.CheckinRow>>(emptyList()) }
    var histOpen by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { checkinHist = BoxClient.checkins(ctx) ?: emptyList() }
    var jotting by remember { mutableStateOf(false) }
    var jotSent by remember { mutableStateOf(false) }
    fun reload() { scope.launch { rows = BoxClient.memoriesList(ctx) } }
    LaunchedEffect(Unit) { reload() }

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
            val y = checkinHist.firstOrNull()
            CheckinCard(yesterday = if (y != null) "${y.day} you felt ${y.feelings}" else "")
        }
        if (checkinHist.isNotEmpty()) item {
            Text(if (histOpen) "[ − past check-ins ]" else "[ + past check-ins (${checkinHist.size}) ]",
                color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { histOpen = !histOpen })
            if (histOpen) Column(Modifier.animateContentSize()) {
                checkinHist.take(14).forEach { r ->
                    Text("${r.day} · ${r.feelings}", color = GhostTextDim,
                        style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.padding(vertical = 2.dp))
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
            OtdYearCard(y)
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
                }
                items(rows!!.filter { memQuery.isBlank() ||
                        it.title.contains(memQuery, true) || it.body.contains(memQuery, true) },
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

/** The DAILY CHECK-IN , "how are you feeling today, and why" , mood chips plus a why prefilled
 *  from what the box already knows about today (framed's places and counts, the journal's notes),
 *  editable before sending. It lands in the JOURNAL via /v1/notes like any other text , synthd
 *  distills it, so feelings become memories with the same sovereignty as everything else. Once a
 *  day: after sending, the card collapses to a checkmark until tomorrow. */
@Composable
private fun CheckinCard(yesterday: String = "") {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    val today = remember { java.text.SimpleDateFormat("yyyy-MM-dd", java.util.Locale.US).format(java.util.Date()) }
    var done by remember { mutableStateOf(com.localghost.app.settings.AppSettings.lastCheckinDay(ctx) == today) }
    var open by remember { mutableStateOf(false) }
    var why by remember { mutableStateOf("") }
    var prefilled by remember { mutableStateOf(false) }
    val base = listOf("calm", "happy", "energised", "grateful", "focused", "proud",
        "tired", "stressed", "anxious", "restless", "low", "lonely")
    var suggested by remember { mutableStateOf<List<String>>(emptyList()) }
    // Suggested-first ordering: the box's GUESSES from the day's shape lead the list, marked with
    // a dot , offered, never preselected. The box proposes, the person disposes.
    val feelings = remember(suggested) { suggested + base.filter { it !in suggested } }
    val picked = remember { mutableStateListOf<String>() }
    if (done) {
        Text("✓ checked in today", color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        return
    }
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Text(if (open) "[ − how are you feeling today? ]" else "[ + how are you feeling today? ]",
            color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.clickable {
                open = !open
                if (open && !prefilled) {
                    prefilled = true
                    scope.launch {
                        val d = BoxClient.daySummary(ctx)
                        if (d != null) suggested = d.suggested
                        if (d != null && why.isEmpty()) {
                            val bits = ArrayList<String>()
                            if (d.sleepMinutes > 0) bits.add("slept ${d.sleepMinutes / 60}h${"%02d".format(d.sleepMinutes % 60)}m")
                            if (d.steps > 0) bits.add("${"%,d".format(d.steps)} steps")
                            if (d.exerciseMinutes >= 10) bits.add("${d.exerciseMinutes}m exercise")
                            if (d.photos > 0) bits.add("${d.photos} photo${if (d.photos == 1) "" else "s"}" +
                                (if (d.places.isNotEmpty()) " around " + d.places.take(2)
                                    .joinToString(" and ") { it.substringAfterLast(" / ") } else ""))
                            if (d.places.isNotEmpty()) bits.add("was at " +
                                d.places.take(3).joinToString("; ") { it.substringAfterLast(" / ") })
                            d.notes.take(2).forEach { bits.add(it) }
                            why = if (bits.isEmpty()) "" else "Today: " + bits.joinToString(". ") + "."
                        }
                    }
                }
            })
        if (open) {
            if (yesterday.isNotEmpty()) {
                Spacer(Modifier.height(4.dp))
                Text(yesterday, color = TerminalDim, style = MaterialTheme.typography.labelMedium)
            }
            Spacer(Modifier.height(8.dp))
            // chips, wrapping rows of 4 , pick up to three, tap again to unpick
            feelings.chunked(4).forEach { rowFeels ->
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    rowFeels.forEach { f ->
                        val on = f in picked
                        val mark = if (f in suggested) "·" else " "
                        Text(if (on) "[$f]" else "$mark$f ",
                            color = if (on) TerminalGreen else GhostTextDim,
                            style = MaterialTheme.typography.labelMedium,
                            modifier = Modifier.clickable {
                                if (on) picked.remove(f) else if (picked.size < 3) picked.add(f)
                            }.padding(vertical = 3.dp))
                    }
                }
            }
            Spacer(Modifier.height(6.dp))
            BasicTextField(why, { why = it },
                textStyle = MaterialTheme.typography.bodySmall.copy(color = GhostText),
                cursorBrush = SolidColor(TerminalGreen),
                decorationBox = { inner -> Box(Modifier.fillMaxWidth().border(1.dp, GhostBorder, RectangleShape)
                    .padding(8.dp)) { if (why.isEmpty()) Text("why? (prefilled from your day , edit freely)",
                        color = TerminalDim, style = MaterialTheme.typography.bodySmall); inner() } },
                modifier = Modifier.fillMaxWidth().heightIn(min = 44.dp))
            Spacer(Modifier.height(6.dp))
            Text("[ save check-in ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable {
                    if (picked.isEmpty() && why.isBlank()) return@clickable
                    scope.launch {
                        val text = "Daily check-in $today\nFeeling: " +
                            (if (picked.isEmpty()) "(unspecified)" else picked.joinToString(", ")) +
                            (if (why.isBlank()) "" else "\nWhy: " + why.trim())
                        if (BoxClient.noteAdd(ctx, text)) {
                            com.localghost.app.settings.AppSettings.setLastCheckinDay(ctx, today)
                            done = true
                        }
                    }
                })
        }
    }
}

@Composable
private fun OtdYearCard(y: BoxClient.OtdYear) {
    val ctx = LocalContext.current
    Column(Modifier.fillMaxWidth().animateContentSize().border(1.dp, GhostBorder, RectangleShape).background(Void).padding(12.dp)) {
        Text("${y.year} , ${if (y.yearsAgo == 1) "1 year" else "${y.yearsAgo} years"} ago",
            color = TerminalGreen, style = MaterialTheme.typography.bodyMedium)
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
            androidx.compose.foundation.lazy.LazyRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                items(y.photos, key = { it }) { hash ->
                    var bmp by remember(hash) { mutableStateOf<android.graphics.Bitmap?>(null) }
                    LaunchedEffect(hash) {
                        bmp = BoxClient.frameThumb(ctx, hash)?.let {
                            android.graphics.BitmapFactory.decodeByteArray(it, 0, it.size)
                        }
                    }
                    bmp?.let {
                        androidx.compose.foundation.Image(
                            bitmap = it.asImageBitmap(),
                            contentDescription = null,
                            modifier = Modifier.size(84.dp).border(1.dp, GhostBorder, RectangleShape))
                    } ?: Box(Modifier.size(84.dp).border(1.dp, GhostBorder, RectangleShape))
                }
            }
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
            if (m.body.isNotBlank()) {
                Spacer(Modifier.height(4.dp))
                Text(m.body, color = GhostTextDim, style = MaterialTheme.typography.bodySmall)
            }
            Spacer(Modifier.height(4.dp))
            Text((if (m.kind == "user") "yours" else "distilled") + " · " +
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
