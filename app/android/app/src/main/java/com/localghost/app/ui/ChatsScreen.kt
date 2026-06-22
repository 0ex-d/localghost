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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.localghost.app.net.Conversation
import com.localghost.app.ui.theme.*

@Composable
fun ChatsScreen(
    conversations: List<Conversation>,
    activeConvId: String?,
    onSelect: (String) -> Unit,
    onNew: () -> Unit,
    onDelete: (String) -> Unit,
) {
    var query by remember { mutableStateOf("") }
    val filtered = remember(conversations, query) {
        if (query.isBlank()) conversations
        else conversations.filter { it.title.contains(query.trim(), ignoreCase = true) }
    }

    Column(Modifier.fillMaxSize().padding(horizontal = 20.dp).padding(top = 20.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SectionLabel("CHATS")
            Spacer(Modifier.weight(1f))
            Text("＋ new", color = TerminalGreen, style = MaterialTheme.typography.labelMedium,
                modifier = Modifier.clickable { onNew() })
        }
        Spacer(Modifier.height(12.dp))

        // search box
        Row(
            Modifier.fillMaxWidth()
                .border(1.dp, GhostBorder, RoundedCornerShape(20.dp))
                .background(VoidLighter, RoundedCornerShape(20.dp))
                .padding(horizontal = 14.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("⌕", color = GhostTextDim, style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(end = 8.dp))
            BasicTextField(
                value = query, onValueChange = { query = it },
                modifier = Modifier.weight(1f),
                textStyle = MaterialTheme.typography.bodyMedium.copy(color = GhostText),
                cursorBrush = SolidColor(TerminalGreen),
                singleLine = true,
                decorationBox = { inner ->
                    if (query.isEmpty())
                        Text("search chats…", color = GhostTextDim,
                            style = MaterialTheme.typography.bodyMedium)
                    inner()
                },
            )
            if (query.isNotEmpty())
                Text("✕", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
                    modifier = Modifier.clickable { query = "" }.padding(start = 8.dp))
        }

        Spacer(Modifier.height(12.dp))

        when {
            conversations.isEmpty() ->
                EmptyLine("no chats yet. start one with ＋ new.")
            filtered.isEmpty() ->
                EmptyLine("no chats match \"${query.trim()}\".")
            else -> LazyColumn(Modifier.fillMaxSize()) {
                items(filtered, key = { it.id }) { c ->
                    ChatRow(c, c.id == activeConvId, onSelect, onDelete)
                }
            }
        }
    }
}

@Composable
private fun ChatRow(c: Conversation, active: Boolean, onSelect: (String) -> Unit, onDelete: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth()
            .border(1.dp, if (active) TerminalDim else GhostBorder, RectangleShape)
            .background(if (active) VoidLighter else Void)
            .clickable { onSelect(c.id) }
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(c.title, color = if (active) TerminalGreen else GhostText,
                style = MaterialTheme.typography.bodyMedium,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
            Spacer(Modifier.height(2.dp))
            Text("${c.updatedLabel} · ${c.messageCount} msgs", color = GhostTextDim,
                style = MaterialTheme.typography.labelMedium)
        }
        Text("✕", color = GhostTextDim, style = MaterialTheme.typography.labelMedium,
            modifier = Modifier.clickable { onDelete(c.id) }.padding(start = 10.dp))
    }
    Spacer(Modifier.height(8.dp))
}
