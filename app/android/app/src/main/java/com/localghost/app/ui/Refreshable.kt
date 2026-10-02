package com.localghost.app.ui

import androidx.compose.foundation.layout.BoxScope
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.pulltorefresh.PullToRefreshDefaults
import androidx.compose.material3.pulltorefresh.rememberPullToRefreshState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.VoidLighter

/**
 * PULL DOWN TO REFRESH, the same on home, CRYPTO and NEWS: the content must scroll (a scrolling
 * column or a list) for the pull to reach it. [refreshing] shows the spinner until the box answers.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun Refreshable(refreshing: Boolean, onRefresh: () -> Unit, modifier: Modifier = Modifier, content: @Composable BoxScope.() -> Unit) {
    val state = rememberPullToRefreshState()
    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = onRefresh,
        modifier = modifier,
        state = state,
        indicator = {
            PullToRefreshDefaults.Indicator(
                state = state, isRefreshing = refreshing, modifier = Modifier.align(Alignment.TopCenter),
                containerColor = VoidLighter, color = TerminalGreen,
            )
        },
        content = content,
    )
}
