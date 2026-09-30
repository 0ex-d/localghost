package com.localghost.app.ui

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.PersistableBundle
import android.view.inputmethod.EditorInfo
import androidx.compose.runtime.Composable
import androidx.compose.ui.ExperimentalComposeUiApi
import androidx.compose.ui.platform.InterceptPlatformTextInput
import androidx.compose.ui.platform.PlatformTextInputMethodRequest
import com.localghost.app.ui.theme.LocalGhostTheme

/**
 * WHAT THE KEYBOARD AND THE CLIPBOARD KEEP (free-time notes, 30 Sep 2026).
 *
 * Everything typed in LocalGhost is personal: questions to the box, check-in notes, memories,
 * searches of your own photos. Gboard and most keyboards learn from what is typed (suggestions,
 * and on some settings a synced dictionary). [PrivateKeyboard] asks every text field in the app
 * for IME_FLAG_NO_PERSONALIZED_LEARNING, what a browser's incognito tab asks for.
 *
 * A copied answer went to the clipboard as plain text: shown in Android's paste preview, kept in
 * the keyboard's clipboard history. [copySensitive] marks it EXTRA_IS_SENSITIVE, which hides the
 * preview and tells keyboards not to keep it.
 */
@OptIn(ExperimentalComposeUiApi::class)
@Composable
fun PrivateKeyboard(content: @Composable () -> Unit) {
    InterceptPlatformTextInput(
        interceptor = { request, next ->
            val quiet = PlatformTextInputMethodRequest { outAttributes ->
                request.createInputConnection(outAttributes).also {
                    outAttributes.imeOptions = outAttributes.imeOptions or EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING
                }
            }
            next.startInputMethod(quiet)
        },
        content = content,
    )
}

/** The app's theme with [PrivateKeyboard] around everything. */
@Composable
fun PrivateTheme(content: @Composable () -> Unit) = LocalGhostTheme { PrivateKeyboard(content) }

/** Copies [text] marked sensitive: no paste preview, not kept in keyboard clipboard histories. */
fun copySensitive(ctx: Context, label: String, text: String) {
    val clip = ClipData.newPlainText(label, text)
    clip.description.extras = PersistableBundle().apply { putBoolean(ClipDescription.EXTRA_IS_SENSITIVE, true) }
    (ctx.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager).setPrimaryClip(clip)
}
