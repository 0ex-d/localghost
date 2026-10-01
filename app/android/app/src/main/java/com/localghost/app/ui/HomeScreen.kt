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
import com.localghost.app.ui.theme.*

/**
 * HOME , where the app opens. BTC and ETH, always, at the box's own price with the day's change
 * and CRYPTO50 under them (the other coins one tap away, on CRYPTO); the day's news in three or
 * four sentences, the box's brief; the most-told stories under it; and a box to ask from, which
 * starts a new chat with the question. Refreshed every minute while it is open.
 */
@Composable
fun HomeScreen(onAsk: (String) -> Unit, onOpenNews: () -> Unit, onOpenCrypto: () -> Unit) {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var news by remember { mutableStateOf<BoxClient.News?>(null) }
    var rates by remember { mutableStateOf<BoxClient.Rates?>(null) }
    var failed by remember { mutableStateOf(false) }
    var nowS by remember { mutableStateOf(System.currentTimeMillis() / 1000) }
    LaunchedEffect(Unit) {
        while (true) {
            val r = BoxClient.rates(ctx)
            val n = BoxClient.news(ctx, since = System.currentTimeMillis() / 1000 - 86_400)
            if (r != null) rates = r
            if (n != null) news = n
            failed = r == null && n == null
            nowS = System.currentTimeMillis() / 1000
            kotlinx.coroutines.delay(60_000)
        }
    }
    val stamp: Long = nowS
    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp)) {
        Column(Modifier.weight(1f).verticalScroll(rememberScrollState())) {
            Spacer(Modifier.height(16.dp))
            Text(java.text.SimpleDateFormat("EEEE d MMMM · HH:mm", java.util.Locale.UK).format(java.util.Date(stamp * 1000L)),
                color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            Spacer(Modifier.height(12.dp))
            PricesCard(rates, failed, onOpenCrypto)
            Spacer(Modifier.height(20.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                SectionLabel("THE DAY'S NEWS")
                Spacer(Modifier.weight(1f))
                Text("news ▸", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { onOpenNews() }.padding(4.dp))
            }
            Spacer(Modifier.height(6.dp))
            val n = news
            when {
                n == null && failed -> ErrorLine("the box did not answer , is it unlocked?")
                n == null -> LoadingRow()
                else -> {
                    if (n.brief.isNotBlank()) {
                        Text(n.brief, color = GhostText, style = MaterialTheme.typography.bodyMedium)
                        Spacer(Modifier.height(4.dp))
                        Text(HomeText.written(n.briefAt, stamp), color = GhostTextDim, style = MaterialTheme.typography.labelSmall)
                    } else {
                        Text(HomeText.noBrief(n.stories.size), color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    }
                    Spacer(Modifier.height(12.dp))
                    val top = HomeBriefText.pick(n.stories.map { s ->
                        HomeBriefText.Story(s.id, s.title, s.summary, s.items.map { it.outlet }, s.lastSeen, s.sources)
                    }, stamp, n = 5)
                    top.forEach { s ->
                        Column(Modifier.fillMaxWidth().clickable { onOpenNews() }.padding(vertical = 6.dp)) {
                            Text(s.title, color = GhostText, style = MaterialTheme.typography.titleSmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            if (s.summary.isNotBlank() && s.summary.trim() != s.title.trim()) {
                                Text(s.summary, color = GhostTextDim, style = MaterialTheme.typography.labelMedium, maxLines = 2, overflow = TextOverflow.Ellipsis)
                            }
                            Text(HomeBriefText.outlets(s.outlets, s.lastSeen, stamp), color = TerminalDim, style = MaterialTheme.typography.labelSmall)
                        }
                    }
                }
            }
            Spacer(Modifier.height(16.dp))
        }
        AskBox(onAsk)
        Spacer(Modifier.height(8.dp))
    }
}

/** BTC and ETH, always; CRYPTO50 under them; a tap opens CRYPTO with the rest. */
@Composable
private fun PricesCard(rates: BoxClient.Rates?, failed: Boolean, onOpenCrypto: () -> Unit) {
    Column(Modifier.fillMaxWidth().border(1.dp, TerminalDim, RectangleShape).background(VoidLighter)
        .clickable { onOpenCrypto() }.padding(14.dp)) {
        val r = rates
        if (r == null) {
            Text(if (failed) "no prices: the box did not answer" else "reading the box's prices…", color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
            return@Column
        }
        val coins: List<HomeText.Coin> = r.index.map { ix -> HomeText.liveOnly(ix.symbol, ix.price, ix.change24) }
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
            val m = r.market
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
 * the box's own price (a minute old) where the box follows it, with the day's change and the
 * market cap.
 */
@Composable
fun CryptoScreen() {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var rates by remember { mutableStateOf<BoxClient.Rates?>(null) }
    var failed by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) {
        while (true) {
            val r = BoxClient.rates(ctx)
            if (r != null) rates = r
            failed = r == null && rates == null
            kotlinx.coroutines.delay(60_000)
        }
    }
    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp).padding(top = 16.dp)) {
        SectionLabel("CRYPTO PRICES")
        Spacer(Modifier.height(6.dp))
        val r = rates
        when {
            r == null && failed -> ErrorLine("the box did not answer , is it unlocked?")
            r == null -> LoadingRow()
            else -> {
                r.market?.takeIf { it.value > 0 }?.let { m ->
                    Text("${m.code} " + "%.1f".format(java.util.Locale.US, m.value) + "  " + HomeText.change(m.dayChange) + " today · ${m.priced} of ${m.constituents} priced · weights of ${m.month}",
                        color = GhostText, style = MaterialTheme.typography.labelMedium)
                    Spacer(Modifier.height(8.dp))
                }
                val live: Map<String, Pair<Double, Double?>> = r.index.associate { ix -> ix.symbol to (ix.price to ix.change24) }
                val ranks: List<HomeText.Coin> = r.ranks.map { c -> HomeText.Coin(c.rank, c.symbol.uppercase(), c.name, c.priceUsd, c.change24, c.marketCap) }
                val rows: List<HomeText.Coin> = HomeText.merge(ranks, live)
                if (rows.isEmpty()) EmptyLine("no rank list on the box yet: Coinbase's is fetched hourly")
                LazyColumn(Modifier.fillMaxSize()) {
                    items(rows, key = { it.symbol + it.rank }) { c ->
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
                }
            }
        }
    }
}
