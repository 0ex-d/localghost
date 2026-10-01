package com.localghost.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.unit.dp
import com.localghost.app.net.BoxClient
import com.localghost.app.sync.BoxFetch
import com.localghost.app.ui.theme.*
import kotlinx.coroutines.launch

/**
 * THE NEWS, as the box tells it: the stories of the last two days, the most-told first, each with
 * the model's grounded summary when it has one and the outlets telling it. The phone fetched the
 * feeds and hands the bytes to the box; the box groups, summarises and keeps them; and when a
 * story is opened it is this phone that opens the article, never the box. A line of the box's
 * market numbers sits at the top (the ECB table, the BTC index) because they come the same way.
 */
@Composable
fun NewsScreen() {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    val scope = rememberCoroutineScope()
    var news by remember { mutableStateOf<BoxClient.News?>(null) }
    var rates by remember { mutableStateOf<BoxClient.Rates?>(null) }
    var failed by remember { mutableStateOf(false) }
    var fetching by remember { mutableStateOf(false) }
    var tick by remember { mutableIntStateOf(0) }
    var open by remember { mutableStateOf<Long?>(null) }
    LaunchedEffect(tick) {
        failed = false
        val n = BoxClient.news(ctx)
        if (n == null) failed = true else news = n
        rates = BoxClient.rates(ctx) ?: rates
    }
    val now = System.currentTimeMillis() / 1000
    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp).padding(top = 20.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SectionLabel("NEWS")
            Spacer(Modifier.weight(1f))
            Text(if (fetching) "fetching…" else "[ fetch now ]", color = if (fetching) GhostTextDim else TerminalGreen,
                style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable(enabled = !fetching) {
                    fetching = true
                    scope.launch {
                        kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { BoxFetch.run(ctx, force = true) }
                        // the box drains its inbox within half a minute; look again then
                        kotlinx.coroutines.delay(35_000)
                        fetching = false
                        tick++
                    }
                })
        }
        Spacer(Modifier.height(6.dp))
        rates?.let { r ->
            val marketLine: String = r.market?.let { m -> NewsText.market(m.value, m.dayChange, m.constituents) } ?: ""
            val prices: List<NewsText.Price> = r.index.map { ix -> NewsText.Price(ix.symbol, ix.price, ix.n, ix.at) }
            Text(NewsText.markets(prices, r.fx, r.fxDay, now, marketLine), color = GhostText, style = MaterialTheme.typography.labelMedium)
            Spacer(Modifier.height(4.dp))
        }
        Text(NewsText.status(news?.lastFetch ?: 0, news?.lastDigest ?: 0, now, fetching), color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
        Spacer(Modifier.height(12.dp))
        val n = news
        when {
            failed && n == null -> ErrorLine("the box did not answer , is it unlocked?")
            n == null -> LoadingRow()
            n.stories.isEmpty() -> EmptyLine("no stories yet , the phone fetches the feeds every two hours while it is on; [ fetch now ] asks now")
            else -> LazyColumn(Modifier.fillMaxSize()) {
                items(n.stories, key = { it.id }) { s ->
                    StoryRow(s, open == s.id, now, onToggle = { open = if (open == s.id) null else s.id },
                        onOpen = { link ->
                            runCatching { ctx.startActivity(android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse(link))) }
                        })
                }
                item { Spacer(Modifier.height(24.dp)) }
            }
        }
    }
}

@Composable
private fun StoryRow(s: BoxClient.NewsStory, open: Boolean, now: Long, onToggle: () -> Unit, onOpen: (String) -> Unit) {
    Column(
        Modifier.fillMaxWidth()
            .border(1.dp, if (open) TerminalDim else GhostBorder, RectangleShape)
            .background(if (open) VoidLighter else Void)
            .clickable { onToggle() }
            .padding(14.dp),
    ) {
        Text(s.title, color = if (s.sources > 1) TerminalGreen else GhostText, style = MaterialTheme.typography.bodyMedium)
        if (s.summary.isNotEmpty()) {
            Spacer(Modifier.height(4.dp))
            Text(s.summary, color = GhostTextDim, style = MaterialTheme.typography.bodySmall)
        }
        Spacer(Modifier.height(4.dp))
        Text(NewsText.outlets(s.items.map { it.outlet }, s.lastSeen, now), color = TerminalDim, style = MaterialTheme.typography.labelMedium)
        if (open) {
            // each outlet's own headline, and the article , opened on this phone, in the browser
            Spacer(Modifier.height(8.dp))
            s.items.forEach { it ->
                Column(Modifier.fillMaxWidth().padding(vertical = 3.dp)) {
                    Text(it.outlet + ": " + it.title, color = GhostText, style = MaterialTheme.typography.labelMedium)
                    if (it.summary.isNotEmpty() && it.summary != s.summary) {
                        Text(it.summary, color = GhostTextDim, style = MaterialTheme.typography.labelMedium)
                    }
                    if (it.link.isNotEmpty()) {
                        Text("[ open at ${it.outlet} ]", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                            modifier = Modifier.clickable { onOpen(it.link) }.padding(vertical = 2.dp))
                    }
                }
            }
            Text("> the article opens on this phone; the box never fetches a page", color = TerminalDim, style = MaterialTheme.typography.labelSmall)
        }
    }
    Spacer(Modifier.height(8.dp))
}
