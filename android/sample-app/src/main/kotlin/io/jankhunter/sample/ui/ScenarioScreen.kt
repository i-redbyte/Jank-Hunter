package io.jankhunter.sample.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.res.stringResource
import io.jankhunter.sample.automatic.ScenarioAction
import io.jankhunter.sample.automatic.ScenarioStep

@Composable
internal fun ScenarioScreen(
    progress: String,
    step: ScenarioStep,
    onAction: (ScenarioAction) -> Unit,
) {
    val actions = step.actions.map { action ->
        SampleAction(label = stringResource(action.labelRes)) {
            onAction(action)
        }
    }
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(SamplePalette.Background)
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .imePadding(),
    ) {
        Column(
            modifier = Modifier
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp, vertical = 28.dp),
        ) {
            SampleEyebrow(progress)
            Spacer(Modifier.height(8.dp))
            Text(
                text = stringResource(step.titleRes),
                color = Color.White,
                fontSize = 30.sp,
                lineHeight = 36.sp,
                fontWeight = FontWeight.Bold,
            )
            Spacer(Modifier.height(12.dp))
            SampleBodyText(stringResource(step.descriptionRes), 16.sp)
            Spacer(Modifier.height(18.dp))
            step.factRes.forEach { factRes ->
                SampleFactCard(stringResource(factRes))
                Spacer(Modifier.height(10.dp))
            }
        }
        if (actions.isNotEmpty()) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(SamplePalette.Background)
                    .padding(start = 20.dp, end = 20.dp, bottom = 24.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                actions.forEach { action ->
                    SampleActionButton(action)
                }
            }
        }
    }
}
