package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Button
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

@Composable
fun LockScreen(error: String?, onUnlock: () -> Unit) {
    Scaffold { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).padding(24.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Text("LocalGhost")
            Spacer(Modifier.height(8.dp))
            Text("The only cloud is you.")
            Spacer(Modifier.height(32.dp))
            Button(onClick = onUnlock) { Text("Unlock") }
            error?.let { Spacer(Modifier.height(16.dp)); Text(it, textAlign = TextAlign.Center) }
        }
    }
}
