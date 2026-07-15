package com.localghost.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.RectangleShape
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.localghost.app.ui.theme.GhostBorder
import com.localghost.app.ui.theme.GhostText
import com.localghost.app.ui.theme.TerminalGreen
import com.localghost.app.ui.theme.VoidLighter

/**
 * Hand-rolled markdown, NO external dependencies. Covers what the local model actually emits:
 *
 *   blocks:  # / ## / ### headers · fenced ``` code blocks · * or - bullets (with [ ]/[x]
 *            checkboxes) · 1. numbered lists · paragraphs
 *   inline:  **bold** · *italic* · `code`
 *
 * Underscores are NEVER emphasis (this app's vocabulary is frame_tags and ghost_rw); links,
 * tables, and anything exotic render as their literal text. Unknown syntax degrading to plain
 * text is the failure mode we want.
 *
 * STREAMING-SAFE BY CONSTRUCTION, the hard requirement (v1 of this file violated it and blanked
 * the whole chat mid-stream). The rules that make it safe:
 *   1. Composable calls cannot sit in try/catch (a Compose language rule), so NOTHING that can
 *      throw runs during composition. All parsing and span computation happens in
 *      remember(text) { runCatching { ... } } , pure code, catchable, cached per text value.
 *   2. Inline styling is computed RANGES applied with addStyle , no push/pop style stack, so
 *      half-arrived markers closing in the wrong order (**a *b , mid-stream) cannot corrupt or
 *      throw. An unclosed marker styles to the end of the text; the next tokens fix it.
 *   3. If parsing fails, or produces nothing while the text is non-blank, the composable falls
 *      back to a plain Text of the raw string. The bubble can NEVER render empty while the model
 *      is saying something.
 */

private sealed class Rendered {
    class Header(val level: Int, val text: AnnotatedString) : Rendered()
    class Paragraph(val text: AnnotatedString) : Rendered()
    class Bullet(val text: AnnotatedString, val marker: String) : Rendered()
    class Numbered(val n: String, val text: AnnotatedString) : Rendered()
    class Code(val text: String) : Rendered()
}

/** "12." at the start of a list line, or null. Bounded to 3 digits. */
private fun numberedPrefix(t: String): String? {
    val digits = t.takeWhile { it.isDigit() }
    if (digits.isEmpty() || digits.length > 3) return null
    if (t.length <= digits.length + 1) return null
    if (t[digits.length] != '.' || t[digits.length + 1] != ' ') return null
    return digits
}

/** Inline spans as computed ranges. Pure, total: no input can make it throw. */
private fun inline(text: String): AnnotatedString {
    val codeStyle = SpanStyle(fontFamily = FontFamily.Monospace, background = VoidLighter)
    val boldStyle = SpanStyle(fontWeight = FontWeight.Bold)
    val italicStyle = SpanStyle(fontStyle = FontStyle.Italic)
    val plain = StringBuilder()
    data class Span(val start: Int, val end: Int, val style: SpanStyle)
    val spans = ArrayList<Span>()
    var boldStart = -1
    var italicStart = -1
    var i = 0
    while (i < text.length) {
        val c = text[i]
        when {
            c == '`' -> {
                val end = text.indexOf('`', i + 1)
                val seg = if (end < 0) text.substring(i + 1) else text.substring(i + 1, end)
                val s = plain.length
                plain.append(seg)
                spans.add(Span(s, plain.length, codeStyle))
                i = if (end < 0) text.length else end + 1
            }
            c == '*' && i + 1 < text.length && text[i + 1] == '*' -> {
                if (boldStart >= 0) {
                    spans.add(Span(boldStart, plain.length, boldStyle)); boldStart = -1
                } else boldStart = plain.length
                i += 2
            }
            c == '*' -> {
                when {
                    italicStart >= 0 -> { spans.add(Span(italicStart, plain.length, italicStyle)); italicStart = -1 }
                    i + 1 < text.length && !text[i + 1].isWhitespace() -> italicStart = plain.length
                    else -> plain.append(c) // "2 * 3" stays arithmetic
                }
                i++
            }
            else -> { plain.append(c); i++ }
        }
    }
    // Unclosed markers mid-stream: style to the end; the closing tokens will arrive.
    if (boldStart >= 0) spans.add(Span(boldStart, plain.length, boldStyle))
    if (italicStart >= 0) spans.add(Span(italicStart, plain.length, italicStyle))
    return buildAnnotatedString {
        append(plain.toString())
        spans.forEach { if (it.end > it.start) addStyle(it.style, it.start, it.end) }
    }
}

/** The full parse: text -> renderable blocks. Pure; only ever called inside runCatching. */
private fun render(src: String): List<Rendered> {
    val out = ArrayList<Rendered>()
    val para = StringBuilder()
    fun flushPara() {
        if (para.isNotBlank()) out.add(Rendered.Paragraph(inline(para.toString().trim())))
        para.setLength(0)
    }
    val lines = src.lines()
    var i = 0
    while (i < lines.size) {
        val t = lines[i].trim()
        when {
            t.startsWith("```") -> { // fenced code , consume to the closing fence or the end
                flushPara()
                val code = StringBuilder()
                i++
                while (i < lines.size && !lines[i].trimStart().startsWith("```")) {
                    if (code.isNotEmpty()) code.append('\n')
                    code.append(lines[i])
                    i++
                }
                out.add(Rendered.Code(code.toString()))
            }
            t.startsWith("#") -> {
                flushPara()
                val level = t.takeWhile { it == '#' }.length.coerceAtMost(3)
                out.add(Rendered.Header(level, inline(t.dropWhile { it == '#' }.trim())))
            }
            t.startsWith("* ") || t.startsWith("- ") -> {
                flushPara()
                var body = t.substring(2).trim()
                var marker = "·"
                if (body.length >= 3 && body[0] == '[' && body[2] == ']' &&
                    (body[1] == ' ' || body[1] == 'x' || body[1] == 'X')
                ) {
                    marker = if (body[1] == ' ') "[ ]" else "[x]"
                    body = body.substring(3).trim()
                }
                out.add(Rendered.Bullet(inline(body), marker))
            }
            numberedPrefix(t) != null -> {
                flushPara()
                val p = numberedPrefix(t)!!
                out.add(Rendered.Numbered(p, inline(t.substring(p.length + 1).trim())))
            }
            t.isEmpty() -> flushPara()
            else -> {
                if (para.isNotEmpty()) para.append(' ')
                para.append(t)
            }
        }
        i++
    }
    flushPara()
    return out
}

/** Renders markdown-shaped model output. Falls back to plain text rather than ever going blank. */
@Composable
fun MarkdownText(text: String, modifier: Modifier = Modifier) {
    // ALL throwing code lives here, in pure land, cached per text value. Composition below only
    // lays out precomputed data and cannot fail.
    val blocks = remember(text) { runCatching { render(text) }.getOrNull() }
    if (blocks == null || (blocks.isEmpty() && text.isNotBlank())) {
        Text(text, color = GhostText, style = MaterialTheme.typography.bodyMedium, modifier = modifier)
        return
    }
    Column(modifier) {
        blocks.forEach { b ->
            when (b) {
                is Rendered.Header -> Text(
                    b.text, color = TerminalGreen,
                    fontWeight = FontWeight.Bold,
                    fontSize = when (b.level) { 1 -> 18.sp; 2 -> 16.sp; else -> 15.sp },
                    modifier = Modifier.padding(top = 8.dp, bottom = 4.dp),
                )
                is Rendered.Paragraph -> Text(
                    b.text, color = GhostText,
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier.padding(vertical = 2.dp),
                )
                is Rendered.Bullet -> Row(Modifier.padding(vertical = 1.dp)) {
                    Text(b.marker, color = TerminalGreen, style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.padding(end = 8.dp))
                    Text(b.text, color = GhostText, style = MaterialTheme.typography.bodyMedium)
                }
                is Rendered.Numbered -> Row(Modifier.padding(vertical = 1.dp)) {
                    Text("${b.n}.", color = TerminalGreen, style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.padding(end = 8.dp))
                    Text(b.text, color = GhostText, style = MaterialTheme.typography.bodyMedium)
                }
                is Rendered.Code -> Text(
                    b.text, color = GhostText, fontFamily = FontFamily.Monospace,
                    style = MaterialTheme.typography.bodyMedium,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 4.dp)
                        .background(VoidLighter)
                        .border(1.dp, GhostBorder, RectangleShape)
                        .padding(8.dp),
                )
            }
        }
    }
}
