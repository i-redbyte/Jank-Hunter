package io.jankhunter.sample

import io.jankhunter.runtime.JankHunterTelemetry

import android.os.Bundle
import android.os.SystemClock
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterComposePhase
import io.jankhunter.sample.ui.SamplePalette
import io.jankhunter.sample.ui.SampleTheme
import kotlinx.coroutines.delay

internal class ComposeJankActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            SampleTheme {
                ComposeJankLabScreen()
            }
        }
    }

    override fun onResume() {
        super.onResume()
        JankHunterTelemetry.setScreen(SCREEN_NAME)
    }

    private companion object {
        const val SCREEN_NAME = "sample.compose_jank"
    }
}

@Composable
private fun ComposeJankLabScreen() {
    val sourceItems = remember { createListItems() }
    val listState = rememberLazyListState()
    var nextRunId by remember { mutableIntStateOf(0) }
    var activeRun by remember { mutableStateOf<ComposeLabRun?>(null) }
    var sharedTick by remember { mutableIntStateOf(0) }
    var lastFinished by remember { mutableStateOf<ComposeLagScenario?>(null) }
    val activeScenario = activeRun?.scenario
    val displayedItems = if (activeScenario == ComposeLagScenario.COMPLEX_LIST) {
        rotateItems(sourceItems, sharedTick)
    } else {
        sourceItems
    }

    LaunchedEffect(activeRun?.id) {
        val run = activeRun ?: return@LaunchedEffect
        JankHunterTelemetry.counter("sample.compose.${run.scenario.stepName}.started", 1)
        try {
            JankHunterTelemetry.traceOperation("$OPERATION_NAME.${run.scenario.stepName}") {
                repeat(run.scenario.iterations) { iteration ->
                    sharedTick = iteration + 1
                    when (run.scenario) {
                        ComposeLagScenario.COMPLEX_LIST -> {
                            if (iteration % LIST_SCROLL_EVERY_TICKS == 0) {
                                listState.scrollToItem(LIST_PREVIEW_COUNT + iteration % LIST_SCROLL_RANGE)
                            }
                        }
                        ComposeLagScenario.HEAVY_DRAW,
                        ComposeLagScenario.HEAVY_MEASURE,
                        -> listState.scrollToItem(0)
                    }
                    delay(FRAME_DELAY_MS)
                }
                JankHunterTelemetry.counter("sample.compose.${run.scenario.stepName}.completed", 1)
                lastFinished = run.scenario
            }
        } finally {
            JankHunter.flush()
            if (activeRun?.id == run.id) activeRun = null
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(SamplePalette.Background)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        ComposeLabHeader(
            activeScenario = activeScenario,
            lastFinished = lastFinished,
            onStart = { scenario ->
                nextRunId += 1
                activeRun = ComposeLabRun(nextRunId, scenario)
            },
        )
        LazyColumn(
            state = listState,
            modifier = Modifier
                .fillMaxWidth()
                .weight(1f),
            contentPadding = PaddingValues(start = 16.dp, top = 8.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item {
                HeavyDrawPreview(
                    enabled = activeScenario == ComposeLagScenario.HEAVY_DRAW,
                    tick = sharedTick,
                )
            }
            item {
                HeavyMeasurePreview(
                    enabled = activeScenario == ComposeLagScenario.HEAVY_MEASURE,
                    tick = sharedTick,
                )
            }
            item {
                Text(
                    text = stringResource(R.string.compose_list_problem_hint),
                    color = SamplePalette.Muted,
                    fontSize = 13.sp,
                    lineHeight = 18.sp,
                )
            }
            items(displayedItems) { item ->
                ProblematicComposeListRow(
                    item = item,
                    sharedTick = sharedTick,
                    expensive = activeScenario == ComposeLagScenario.COMPLEX_LIST,
                )
            }
        }
    }
}

@Composable
private fun ComposeLabHeader(
    activeScenario: ComposeLagScenario?,
    lastFinished: ComposeLagScenario?,
    onStart: (ComposeLagScenario) -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(R.string.compose_lab_title),
            color = Color.White,
            fontSize = 26.sp,
            lineHeight = 32.sp,
            fontWeight = FontWeight.Bold,
        )
        Text(
            text = stringResource(R.string.compose_lab_description),
            color = SamplePalette.Muted,
            fontSize = 14.sp,
            lineHeight = 19.sp,
        )
        val status = when {
            activeScenario != null -> stringResource(R.string.compose_lab_running, stringResource(activeScenario.labelRes))
            lastFinished != null -> stringResource(R.string.compose_lab_finished, stringResource(lastFinished.labelRes))
            else -> stringResource(R.string.compose_lab_idle)
        }
        Text(text = status, color = SamplePalette.Cyan, fontSize = 14.sp)
        ComposeScenarioButton(ComposeLagScenario.COMPLEX_LIST, onStart)
        ComposeScenarioButton(ComposeLagScenario.HEAVY_DRAW, onStart)
        ComposeScenarioButton(ComposeLagScenario.HEAVY_MEASURE, onStart)
    }
}

@Composable
private fun ComposeScenarioButton(
    scenario: ComposeLagScenario,
    onStart: (ComposeLagScenario) -> Unit,
) {
    Button(
        onClick = { onStart(scenario) },
        modifier = Modifier
            .fillMaxWidth()
            .height(44.dp),
    ) {
        Text(text = stringResource(scenario.labelRes), fontSize = 13.sp)
    }
}

@Composable
private fun HeavyDrawPreview(enabled: Boolean, tick: Int) {
    val label = stringResource(R.string.compose_draw_preview)
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(PREVIEW_HEIGHT_DP.dp)
            .background(SamplePalette.Panel, RoundedCornerShape(10.dp))
            .border(1.dp, SamplePalette.Border, RoundedCornerShape(10.dp)),
    ) {
        Canvas(Modifier.fillMaxSize()) {
            if (enabled) {
                JankHunterTelemetry.traceCompose(
                    JankHunterComposePhase.DRAW,
                    "sample.compose.expensive_canvas",
                ) {
                    burnComposeCpu(DRAW_WORK_MS)
                    repeat(DRAW_SHAPE_COUNT) { index ->
                        val fraction = (index + tick % DRAW_SHAPE_COUNT) / DRAW_SHAPE_COUNT.toFloat()
                        drawCircle(
                            color = if (index % 2 == 0) SamplePalette.Cyan else SamplePalette.Emphasis,
                            radius = size.minDimension * fraction / 2f,
                            center = center,
                        )
                    }
                }
            }
        }
        Text(
            text = label,
            color = Color.White,
            modifier = Modifier
                .align(Alignment.Center)
                .padding(12.dp),
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
private fun HeavyMeasurePreview(enabled: Boolean, tick: Int) {
    val label = stringResource(R.string.compose_measure_preview)
    val measurementIteration = tick
    Layout(
        content = {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .background(SamplePalette.Panel, RoundedCornerShape(10.dp))
                    .border(1.dp, SamplePalette.Border, RoundedCornerShape(10.dp)),
                contentAlignment = Alignment.Center,
            ) {
                Text(text = "$label · $tick", color = Color.White, fontWeight = FontWeight.Bold)
            }
        },
        modifier = Modifier
            .fillMaxWidth()
            .height(PREVIEW_HEIGHT_DP.dp),
    ) { measurables, constraints ->
        // Keep the changing state in the measure policy. Otherwise Compose can reuse the previous
        // measurement when the child's size is stable and the lab records only its first pass.
        if (enabled && measurementIteration >= 0) {
            JankHunterTelemetry.traceCompose(
                JankHunterComposePhase.MEASURE,
                "sample.compose.expensive_measure_policy",
            ) {
                burnComposeCpu(MEASURE_WORK_MS)
            }
        }
        val placeable = measurables.single().measure(constraints)
        layout(placeable.width, placeable.height) {
            placeable.placeRelative(0, 0)
        }
    }
}

@Composable
private fun ProblematicComposeListRow(
    item: ComposeListItem,
    sharedTick: Int,
    expensive: Boolean,
) {
    val detail = if (expensive) {
        // The Gradle plugin already measures this named @Composable boundary. An additional manual
        // composition trace would describe the same work twice in the report.
        burnComposeCpu(LIST_ITEM_WORK_MS)
        item.details.joinToString(separator = " · ") { value -> "$value:${sharedTick % LIST_VALUE_MODULO}" }
    } else {
        item.details.joinToString(separator = " · ")
    }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(SamplePalette.Panel, RoundedCornerShape(8.dp))
            .border(1.dp, SamplePalette.Border, RoundedCornerShape(8.dp))
            .padding(12.dp),
    ) {
        Text(text = item.title, color = Color.White, fontWeight = FontWeight.Bold)
        Text(text = detail, color = SamplePalette.Muted, fontSize = 12.sp, lineHeight = 16.sp)
    }
}

private fun createListItems(): List<ComposeListItem> {
    return List(LIST_ITEM_COUNT) { index ->
        ComposeListItem(
            id = index,
            title = "Сложная строка #$index",
            details = mutableListOf("статус", "цена", "аватар", "счётчик"),
        )
    }
}

private fun rotateItems(items: List<ComposeListItem>, tick: Int): List<ComposeListItem> {
    if (items.isEmpty()) return items
    val offset = tick % items.size
    return items.drop(offset) + items.take(offset)
}

private fun burnComposeCpu(durationMs: Long) {
    val deadline = SystemClock.elapsedRealtimeNanos() + durationMs * NANOS_PER_MS
    var value = 1.0
    while (SystemClock.elapsedRealtimeNanos() < deadline) {
        repeat(CPU_BATCH_SIZE) { index ->
            value = value * CPU_MULTIPLIER + index
            if (value > CPU_RESET_THRESHOLD) value /= CPU_RESET_DIVISOR
        }
    }
}

private class ComposeListItem(
    val id: Int,
    val title: String,
    val details: MutableList<String>,
)

private data class ComposeLabRun(val id: Int, val scenario: ComposeLagScenario)

private enum class ComposeLagScenario(
    val stepName: String,
    val labelRes: Int,
    val iterations: Int,
) {
    COMPLEX_LIST("complex_list_recomposition_storm", R.string.compose_action_complex_list, 36),
    HEAVY_DRAW("expensive_compose_draw", R.string.compose_action_heavy_draw, 18),
    HEAVY_MEASURE("expensive_compose_measure", R.string.compose_action_heavy_measure, 20),
}

private const val OPERATION_NAME = "sample.compose_jank"
private const val FRAME_DELAY_MS = 16L
private const val LIST_SCROLL_EVERY_TICKS = 3
private const val LIST_SCROLL_RANGE = 30
private const val LIST_PREVIEW_COUNT = 3
private const val LIST_ITEM_COUNT = 120
private const val LIST_VALUE_MODULO = 10
private const val LIST_ITEM_WORK_MS = 3L
private const val DRAW_WORK_MS = 48L
private const val MEASURE_WORK_MS = 42L
private const val PREVIEW_HEIGHT_DP = 132
private const val DRAW_SHAPE_COUNT = 64
private const val NANOS_PER_MS = 1_000_000L
private const val CPU_BATCH_SIZE = 96
private const val CPU_MULTIPLIER = 1.000_000_3
private const val CPU_RESET_THRESHOLD = 1_000_000_000.0
private const val CPU_RESET_DIVISOR = 7.0
