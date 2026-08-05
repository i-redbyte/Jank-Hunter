package io.jankhunter.sample.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.TextUnit
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

@Composable
internal fun SamplePanel(content: @Composable () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(SamplePalette.Panel, RoundedCornerShape(8.dp))
            .border(1.dp, SamplePalette.Cyan, RoundedCornerShape(8.dp))
            .padding(16.dp),
        content = { content() },
    )
}

@Composable
internal fun SampleFactCard(value: String) {
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .background(SamplePalette.Panel, RoundedCornerShape(10.dp))
            .border(1.dp, SamplePalette.Border, RoundedCornerShape(10.dp))
            .padding(horizontal = 16.dp, vertical = 14.dp),
    ) {
        Text(
            text = value,
            color = Color.White,
            fontSize = 15.sp,
            lineHeight = 20.sp,
        )
    }
}

@Composable
internal fun SampleEyebrow(value: String) {
    Text(
        text = value.uppercase(),
        color = SamplePalette.Cyan,
        fontSize = 12.sp,
        lineHeight = 16.sp,
        fontWeight = FontWeight.Bold,
    )
}

@Composable
internal fun SampleBodyText(value: String, size: TextUnit = 14.sp) {
    Text(
        text = value,
        color = SamplePalette.Muted,
        fontSize = size,
        lineHeight = size * 1.35f,
    )
}

@Composable
internal fun SampleActionButton(action: SampleAction) {
    val background = when (action.tone) {
        SampleActionTone.PRIMARY -> SamplePalette.Cyan
        SampleActionTone.SUCCESS -> SamplePalette.Success
        SampleActionTone.WARNING -> SamplePalette.Warning
        SampleActionTone.DANGER -> SamplePalette.Danger
        SampleActionTone.EMPHASIS -> SamplePalette.Emphasis
    }
    val foreground = if (action.tone == SampleActionTone.PRIMARY) Color.Black else Color.White
    Button(
        onClick = action.onClick,
        modifier = Modifier
            .fillMaxWidth()
            .height(48.dp),
        shape = RoundedCornerShape(8.dp),
        colors = ButtonDefaults.buttonColors(
            containerColor = background,
            contentColor = foreground,
        ),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
    ) {
        Text(
            text = action.label,
            fontSize = 14.sp,
            lineHeight = 18.sp,
            fontWeight = FontWeight.Medium,
        )
    }
}
