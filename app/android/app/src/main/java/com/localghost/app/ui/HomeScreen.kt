package com.localghost.app.ui

import androidx.compose.foundation.layout.*
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

@Composable
fun HomeScreen() {
    Scaffold { pad ->
        Column(Modifier.fillMaxSize().padding(pad).padding(24.dp)) {
            Text("Connected")
            Spacer(Modifier.height(8.dp))
            // The persona's own marker + data render here once the box APIs land.
            // You recognise which persona you're in by YOUR content/marker — never
            // by a status badge the box doesn't know to hide from a coercer.
            Text("Your memory layer loads here.")
        }
    }
}
