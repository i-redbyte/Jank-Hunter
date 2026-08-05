package io.jankhunter.sample.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

internal object SamplePalette {
    val Background = Color(0xFF070A12)
    val Panel = Color(0xFF0C1222)
    val Border = Color(0xFF253952)
    val Muted = Color(0xFFA6B4CC)
    val Cyan = Color(0xFF6FF7FF)
    val Success = Color(0xFF2FA66F)
    val Warning = Color(0xFFB0841F)
    val Danger = Color(0xFFBA3654)
    val Emphasis = Color(0xFF9C46BE)
}

@Composable
internal fun SampleTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = SamplePalette.Cyan,
            background = SamplePalette.Background,
            surface = SamplePalette.Panel,
            onPrimary = Color.Black,
            onBackground = Color.White,
            onSurface = Color.White,
        ),
        content = content,
    )
}
