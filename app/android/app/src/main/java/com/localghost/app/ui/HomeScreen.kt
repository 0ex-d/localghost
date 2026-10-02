package com.localghost.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.localghost.app.net.BoxClient
import com.localghost.app.phrases.HomeBriefText
import com.localghost.app.sync.BoxFetch
import com.localghost.app.ui.theme.*
import kotlinx.coroutines.launch

/**
 * HOME , where the app opens. BTC and ETH, always, at the box's own price (Coinbase's last trade,
 * asked every five seconds while home is open) with the day's change and CRYPTO50 under them (the
 * other coins one tap away, on CRYPTO); the day's news as one point per story, the box's brief,
 * each point opening its story in NEWS; and a box to ask from, which starts a new chat with the
 * question. Pull down to refresh: the box's copy at once, and the feeds fetched when they are half
 * an hour old. Read again every minute while it is open.
 */
@Composable
fun HomeScreen(onAsk: (String) -> Unit, onOpenNews: () -> Unit, onOpenStory: (Long) -> Unit, onOpenCrypto: () -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    var news by remember { mutableStateOf<BoxClient.News?>(null) }
    var rates by remember { mutableStateOf<BoxClient.Rates?>(null) }
    var fast by remember { mutableStateOf<Map<String, Pair<Double, Double?>>>(emptyMap()) }
    var failed by remember { mutableStateOf(false) }
    var refreshing by remember { mutableStateOf(false) }
    var fetching by remember { mutableStateOf(false) }
    var tick by remember { mutableIntStateOf(0) }
    var nowS by remember { mutableStateOf(System.currentTimeMillis() / 1000) }
    LaunchedEffect(tick) {
        while (true) {
            val r = BoxClient.rates(ctx)
            val n = BoxClient.news(ctx, since = System.currentTimeMillis() / 1000 - 86_400)
            if (r != null) rates = r
            if (n != null) news = n
            failed = r == null && n == null
            nowS = System.currentTimeMillis() / 1000
            refreshing = false
            kotlinx.coroutines.delay(60_000)
        }
    }
    // BTC and ETH every five seconds, from the box's Redis (no Postgres, no venue per ask)
    LaunchedEffect(Unit) {
        while (true) {
            BoxClient.fast(ctx)?.let { fast = it }
            kotlinx.coroutines.delay(5_000)
        }
    }
    val refresh: () -> Unit = {
        refreshing = true
        tick++
        if (!fetching && NewsText.wantsFetch(news?.lastFetch ?: 0, System.currentTimeMillis() / 1000)) {
            fetching = true
            scope.launch {
                kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { BoxFetch.run(ctx, force = true) }
                kotlinx.coroutines.delay(35_000) // the box takes the batch within half a minute
                fetching = false
                tick++
            }
        }
    }
    val stamp: Long = nowS
    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp)) {
        Refreshable(refreshing, refresh, Modifier.weight(1f).fillMaxWidth()) {
            Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                Spacer(Modifier.height(16.dp))
                Text(java.text.SimpleDateFormat("EEEE d MMMM · HH:mm", java.util.Locale.UK).format(java.util.Date(stamp * 1000L)) +
                    (if (fetching) " · fetching the feeds…" else ""),
                    color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                Spacer(Modifier.height(12.dp))
                PricesCard(rates, fast, failed, onOpenCrypto)
                Spacer(Modifier.height(20.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    SectionLabel("THE DAY'S NEWS")
                    Spacer(Modifier.weight(1f))
                    Text("all stories ▸", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                        modifier = Modifier.clickable { onOpenNews() }.padding(4.dp))
                }
                Spacer(Modifier.height(6.dp))
                val n = news
                when {
                    n == null && failed -> ErrorLine("the box did not answer , is it unlocked?")
                    n == null -> LoadingRow()
                    else -> {
                        val points: List<HomeText.Point> = HomeText.points(n.brief, n.briefStories)
                        if (points.isNotEmpty()) {
                            points.forEach { p ->
                                BriefPoint(p) {
                                    val id = p.story
                                    if (id != null) onOpenStory(id) else onOpenNews()
                                }
                            }
                            Spacer(Modifier.height(4.dp))
                            Text(HomeText.written(n.briefAt, stamp), color = GhostTextDim, style = MaterialTheme.typography.labelSmall)
                        } else {
                            Text(HomeText.noBrief(n.stories.size), color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                            Spacer(Modifier.height(8.dp))
                            // no brief yet: the most-told stories, each opening in NEWS
                            val top = HomeBriefText.pick(n.stories.map { s ->
                                HomeBriefText.Story(s.id, s.title, s.summary, s.items.map { it.outlet }, s.lastSeen, s.sources)
                            }, stamp, n = 5)
                            top.forEach { s ->
                                Column(Modifier.fillMaxWidth().clickable { onOpenStory(s.id) }.padding(vertical = 6.dp)) {
                                    Text(s.title, color = GhostText, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                    val lead = NewsText.lead(s.summary)
                                    if (lead.isNotBlank() && lead != s.title.trim()) {
                                        Text(lead, color = GhostTextDim, style = MaterialTheme.typography.labelMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                                    }
                                    Text(HomeBriefText.outlets(s.outlets, s.lastSeen, stamp), color = TerminalDim, style = MaterialTheme.typography.labelSmall)
                                }
                            }
                        }
                    }
                }
                Spacer(Modifier.height(16.dp))
            }
        }
        AskBox(onAsk)
        Spacer(Modifier.height(8.dp))
    }
}

/** One point of the brief: a green dot, the sentence, and a chevron when it opens its story. */
@Composable
private fun BriefPoint(p: HomeText.Point, onTap: () -> Unit) {
    Row(Modifier.fillMaxWidth().clickable { onTap() }.padding(vertical = 7.dp), verticalAlignment = Alignment.Top) {
        Text("•", color = TerminalGreen, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.width(16.dp))
        Text(p.text, color = GhostText, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        if (p.story != null) Text(" ›", color = TerminalDim, style = MaterialTheme.typography.bodyMedium)
    }
}

/** BTC and ETH, always; CRYPTO50 under them; a tap opens CRYPTO with the rest. */
@Composable
private fun PricesCard(rates: BoxClient.Rates?, fast: Map<String, Pair<Double, Double?>>, failed: Boolean, onOpenCrypto: () -> Unit) {
    Column(Modifier.fillMaxWidth().border(1.dp, TerminalDim, RectangleShape).background(VoidLighter)
        .clickable { onOpenCrypto() }.padding(14.dp)) {
        val r = rates
        if (r == null && fast.isEmpty()) {
            Text(if (failed) "no prices: the box did not answer" else "reading the box's prices…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            return@Column
        }
        val live: Map<String, Pair<Double, Double?>> = HomeText.withFast(r?.index?.associate { ix -> ix.symbol to (ix.price to ix.change24) } ?: emptyMap(), fast)
        val coins: List<HomeText.Coin> = live.map { (sym, v) -> HomeText.liveOnly(sym, v.first, v.second) }
        val pinned: List<HomeText.Coin> = HomeText.pinned(coins)
        if (pinned.isEmpty()) Text("no prices on the box yet", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        pinned.forEach { c ->
            Row(verticalAlignment = Alignment.Bottom, modifier = Modifier.padding(vertical = 2.dp)) {
                Text(c.symbol, color = TerminalGreen, style = MaterialTheme.typography.titleMedium, modifier = Modifier.width(56.dp))
                Text(HomeText.money(c.usd), color = GhostText, fontSize = 22.sp, style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
                val ch = HomeText.change(c.change24)
                Text(ch, color = if (ch.startsWith("-")) Warning else TerminalGreen, style = MaterialTheme.typography.titleSmall)
            }
        }
        Spacer(Modifier.height(6.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            val m = r?.market
            Text(if (m != null && m.value > 0) "${m.code} " + "%.1f".format(java.util.Locale.US, m.value) + "  " + HomeText.change(m.dayChange) + " today" else "",
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium, modifier = Modifier.weight(1f))
            Text("prices ▸", color = TerminalGreen, style = MaterialTheme.typography.labelMedium)
        }
    }
}

/** The question box: what is typed here starts a new chat. */
@Composable
private fun AskBox(onAsk: (String) -> Unit) {
    var input by remember { mutableStateOf("") }
    val canSend = input.isNotBlank()
    Row(Modifier.fillMaxWidth().border(1.dp, GhostBorder, RoundedCornerShape(22.dp))
        .background(VoidLighter, RoundedCornerShape(22.dp)).padding(horizontal = 10.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically) {
        BasicTextField(
            value = input, onValueChange = { input = it },
            modifier = Modifier.weight(1f).padding(horizontal = 6.dp, vertical = 10.dp),
            textStyle = MaterialTheme.typography.bodyMedium.copy(color = GhostText),
            cursorBrush = SolidColor(TerminalGreen),
            maxLines = 4,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
            keyboardActions = KeyboardActions(onSend = { if (canSend) { onAsk(input.trim()); input = "" } }),
            decorationBox = { inner ->
                if (input.isEmpty()) Text("ask your box…", color = GhostTextDim, style = MaterialTheme.typography.bodyMedium)
                inner()
            },
        )
        Box(Modifier.size(36.dp).clip(CircleShape).background(if (canSend) TerminalGreen else VoidLighter)
            .clickable(enabled = canSend) { onAsk(input.trim()); input = "" },
            contentAlignment = Alignment.Center) {
            Text("›", color = if (canSend) Void else GhostTextDim, fontSize = 20.sp)
        }
    }
}

/**
 * CRYPTO , the coins behind home's two: CRYPTO50, then the fifty largest Coinbase lists, each at
 * the box's own price (BTC, ETH and SOL seconds old, the rest a minute old) where the box follows
 * it, with the day's change and the market cap (the shown price times Coinbase's circulating
 * supply). Pull down to read the box again.
 */
@Composable
fun CryptoScreen() {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var rates by remember { mutableStateOf<BoxClient.Rates?>(null) }
    var fast by remember { mutableStateOf<Map<String, Pair<Double, Double?>>>(emptyMap()) }
    var failed by remember { mutableStateOf(false) }
    var refreshing by remember { mutableStateOf(false) }
    var tick by remember { mutableIntStateOf(0) }
    LaunchedEffect(tick) {
        while (true) {
            val r = BoxClient.rates(ctx)
            if (r != null) rates = r
            failed = r == null && rates == null
            refreshing = false
            kotlinx.coroutines.delay(60_000)
        }
    }
    LaunchedEffect(Unit) {
        while (true) {
            BoxClient.fast(ctx)?.let { fast = it }
            kotlinx.coroutines.delay(5_000)
        }
    }
    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp).padding(top = 16.dp)) {
        SectionLabel("CRYPTO PRICES")
        Spacer(Modifier.height(6.dp))
        Refreshable(refreshing, { refreshing = true; tick++ }, Modifier.weight(1f).fillMaxWidth()) {
            val r = rates
            val live: Map<String, Pair<Double, Double?>> = HomeText.withFast(r?.index?.associate { ix -> ix.symbol to (ix.price to ix.change24) } ?: emptyMap(), fast)
            val ranks: List<HomeText.Coin> = r?.ranks?.map { c -> HomeText.Coin(c.rank, c.symbol.uppercase(), c.name, c.priceUsd, c.change24, c.marketCap, c.supply) } ?: emptyList()
            val rows: List<HomeText.Coin> = HomeText.merge(ranks, live)
            LazyColumn(Modifier.fillMaxSize()) {
                when {
                    r == null && failed -> item { ErrorLine("the box did not answer , is it unlocked? pull down to try again") }
                    r == null -> item { LoadingRow() }
                    else -> {
                        r.market?.takeIf { it.value > 0 }?.let { m ->
                            item {
                                Text("${m.code} " + "%.1f".format(java.util.Locale.US, m.value) + "  " + HomeText.change(m.dayChange) + " today · ${m.priced} of ${m.constituents} priced · weights of ${m.month}",
                                    color = GhostText, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(bottom = 8.dp))
                            }
                        }
                        if (rows.isEmpty()) item { EmptyLine("no rank list on the box yet: Coinbase's is fetched hourly") }
                        items(rows, key = { it.symbol + it.rank }) { c -> CoinLine(c) }
                    }
                }
            }
        }
    }
}

@Composable
private fun CoinLine(c: HomeText.Coin) {
    Row(Modifier.fillMaxWidth().padding(vertical = 5.dp), verticalAlignment = Alignment.CenterVertically) {
        Text("${c.rank}", color = GhostTextDim, style = MaterialTheme.typography.labelSmall, modifier = Modifier.width(28.dp))
        Column(Modifier.weight(1f)) {
            Text(c.symbol, color = TerminalGreen, style = MaterialTheme.typography.titleSmall)
            Text(c.name + (if (c.cap > 0) " · " + HomeText.cap(c.cap) else ""), color = GhostTextDim,
                style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        Column(horizontalAlignment = Alignment.End) {
            Text(HomeText.money(c.usd), color = GhostText, style = MaterialTheme.typography.bodyMedium)
            val ch = HomeText.change(c.change24)
            Text(ch, color = if (ch.startsWith("-")) Warning else TerminalGreen, style = MaterialTheme.typography.labelSmall)
        }
    }
}
