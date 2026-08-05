package io.jankhunter.sample.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

@Composable
internal fun ManualControlsScreen(
    label: String,
    title: String,
    description: String,
    status: String,
    sections: List<SampleSection>,
) {
    LazyColumn(
        modifier = Modifier
            .fillMaxSize()
            .background(SamplePalette.Background)
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .imePadding(),
        contentPadding = PaddingValues(start = 18.dp, top = 18.dp, end = 18.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        item {
            SamplePanel {
                SampleEyebrow(label)
                Spacer(Modifier.height(4.dp))
                Text(
                    text = title,
                    color = Color.White,
                    fontSize = 28.sp,
                    lineHeight = 34.sp,
                    fontWeight = FontWeight.Bold,
                )
                Spacer(Modifier.height(6.dp))
                SampleBodyText(description)
                Spacer(Modifier.height(8.dp))
                SampleBodyText(status)
            }
        }
        items(sections) { section ->
            SamplePanel {
                SampleEyebrow(section.title)
                Spacer(Modifier.height(6.dp))
                SampleBodyText(section.subtitle)
                section.detail?.let { detail ->
                    Spacer(Modifier.height(8.dp))
                    SampleBodyText(detail)
                }
                Spacer(Modifier.height(8.dp))
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    section.actions.forEach { action ->
                        SampleActionButton(action)
                    }
                }
            }
        }
    }
}
