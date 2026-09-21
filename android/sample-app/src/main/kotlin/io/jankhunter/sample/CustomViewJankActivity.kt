package io.jankhunter.sample

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.os.Bundle
import android.os.SystemClock
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import androidx.activity.ComponentActivity
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterTelemetry
import kotlin.math.cos
import kotlin.math.sin

private const val CUSTOM_VIEW_SCREEN_NAME = "sample.custom_view_jank"
private const val CUSTOM_VIEW_OPERATION_NAME = "sample.custom_view_jank"

internal class CustomViewJankActivity : ComponentActivity() {
    private lateinit var jankView: CustomJankView
    private lateinit var statusView: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(createContent())
    }

    override fun onResume() {
        super.onResume()
        JankHunterTelemetry.setScreen(CUSTOM_VIEW_SCREEN_NAME)
    }

    override fun onDestroy() {
        jankView.cancelScenario()
        super.onDestroy()
    }

    private fun createContent(): View {
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_HORIZONTAL
            setPadding(dp(PAGE_PADDING_DP), dp(PAGE_PADDING_DP), dp(PAGE_PADDING_DP), dp(PAGE_PADDING_DP))
            setBackgroundColor(PAGE_BACKGROUND)
        }
        content.addView(label(R.string.custom_view_lab_title, TITLE_TEXT_SIZE_SP, Color.WHITE))
        content.addView(label(R.string.custom_view_lab_description, BODY_TEXT_SIZE_SP, SECONDARY_TEXT))
        statusView = label(R.string.custom_view_lab_idle, BODY_TEXT_SIZE_SP, ACCENT)
        content.addView(statusView)
        jankView = CustomJankView(this, ::onScenarioFinished)
        content.addView(
            jankView,
            LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, dp(PREVIEW_HEIGHT_DP)).apply {
                topMargin = dp(ITEM_SPACING_DP)
                bottomMargin = dp(ITEM_SPACING_DP)
            },
        )
        addScenarioButton(content, R.string.custom_view_action_heavy_draw, CustomViewLagScenario.HEAVY_DRAW)
        addScenarioButton(content, R.string.custom_view_action_allocations, CustomViewLagScenario.ALLOCATION_CHURN)
        addScenarioButton(content, R.string.custom_view_action_layout_storm, CustomViewLagScenario.LAYOUT_STORM)
        content.addView(label(R.string.custom_view_hint, CAPTION_TEXT_SIZE_SP, SECONDARY_TEXT))

        return ScrollView(this).apply { addView(content) }
    }

    private fun addScenarioButton(
        parent: LinearLayout,
        labelRes: Int,
        scenario: CustomViewLagScenario,
    ) {
        parent.addView(
            Button(this).apply {
                setText(labelRes)
                isAllCaps = false
                setOnClickListener {
                    statusView.text = getString(R.string.custom_view_lab_running, getString(labelRes))
                    jankView.startScenario(scenario)
                }
            },
            LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
                bottomMargin = dp(BUTTON_SPACING_DP)
            },
        )
    }

    private fun onScenarioFinished(scenario: CustomViewLagScenario) {
        statusView.text = getString(R.string.custom_view_lab_finished, getString(scenario.labelRes))
    }

    private fun label(textRes: Int, textSizeSp: Float, textColor: Int): TextView {
        return TextView(this).apply {
            setText(textRes)
            textSize = textSizeSp
            setTextColor(textColor)
            setPadding(0, dp(TEXT_VERTICAL_PADDING_DP), 0, dp(TEXT_VERTICAL_PADDING_DP))
        }
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

    private companion object {
        const val PAGE_PADDING_DP = 20
        const val PREVIEW_HEIGHT_DP = 300
        const val ITEM_SPACING_DP = 16
        const val BUTTON_SPACING_DP = 8
        const val TEXT_VERTICAL_PADDING_DP = 4
        const val TITLE_TEXT_SIZE_SP = 28f
        const val BODY_TEXT_SIZE_SP = 15f
        const val CAPTION_TEXT_SIZE_SP = 13f
        const val PAGE_BACKGROUND = 0xFF071A20.toInt()
        const val SECONDARY_TEXT = 0xFFB8C8C8.toInt()
        const val ACCENT = 0xFF79E6D0.toInt()
    }
}

private enum class CustomViewLagScenario(
    val stepName: String,
    val ownerName: String,
    val labelRes: Int,
    val frameCount: Int,
) {
    HEAVY_DRAW(
        stepName = "heavy_on_draw",
        ownerName = "sample.custom_view.onDraw.heavy_computation",
        labelRes = R.string.custom_view_action_heavy_draw,
        frameCount = 10,
    ),
    ALLOCATION_CHURN(
        stepName = "allocations_in_on_draw",
        ownerName = "sample.custom_view.onDraw.allocation_churn",
        labelRes = R.string.custom_view_action_allocations,
        frameCount = 18,
    ),
    LAYOUT_STORM(
        stepName = "layout_and_invalidate_storm",
        ownerName = "sample.custom_view.onMeasure.layout_storm",
        labelRes = R.string.custom_view_action_layout_storm,
        frameCount = 24,
    ),
}

private class CustomJankView(
    context: Context,
    private val onFinished: (CustomViewLagScenario) -> Unit,
) : View(context) {
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.rgb(83, 220, 188)
        style = Paint.Style.STROKE
        strokeWidth = resources.displayMetrics.density * STROKE_WIDTH_DP
    }
    private var scenario: CustomViewLagScenario? = null
    private var remainingFrames = 0
    private var checksum = 0.0

    init {
        setBackgroundColor(Color.rgb(9, 39, 45))
        contentDescription = context.getString(R.string.custom_view_lab_title)
    }

    fun startScenario(nextScenario: CustomViewLagScenario) {
        cancelScenario()
        scenario = nextScenario
        remainingFrames = nextScenario.frameCount
        withScenarioContext(nextScenario) {
            JankHunterTelemetry.counter("sample.custom_view.${nextScenario.stepName}.started", 1)
        }
        postInvalidateOnAnimation()
    }

    fun cancelScenario() {
        scenario = null
        remainingFrames = 0
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        val activeScenario = scenario
        if (activeScenario == CustomViewLagScenario.LAYOUT_STORM) {
            withScenarioContext(activeScenario) {
                JankHunterTelemetry.withOwner(activeScenario.ownerName, Runnable { burnCpu(LAYOUT_WORK_MS) })
            }
        }
        super.onMeasure(widthMeasureSpec, heightMeasureSpec)
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        drawReferenceGrid(canvas)
        val activeScenario = scenario ?: return
        withScenarioContext(activeScenario) {
            JankHunterTelemetry.withOwner(activeScenario.ownerName, Runnable {
                when (activeScenario) {
                    CustomViewLagScenario.HEAVY_DRAW -> drawHeavyFrame(canvas)
                    CustomViewLagScenario.ALLOCATION_CHURN -> drawAllocationHeavyFrame(canvas)
                    CustomViewLagScenario.LAYOUT_STORM -> requestLayout()
                }
            })
        }
        remainingFrames -= 1
        if (remainingFrames > 0) {
            postInvalidateOnAnimation()
        } else {
            finishScenario(activeScenario)
        }
    }

    override fun onDetachedFromWindow() {
        cancelScenario()
        super.onDetachedFromWindow()
    }

    private fun finishScenario(finishedScenario: CustomViewLagScenario) {
        withScenarioContext(finishedScenario) {
            JankHunterTelemetry.counter("sample.custom_view.${finishedScenario.stepName}.completed", 1)
        }
        scenario = null
        JankHunter.flush()
        onFinished(finishedScenario)
    }

    private inline fun withScenarioContext(activeScenario: CustomViewLagScenario, block: () -> Unit) {
        JankHunterTelemetry.withContext(
            CUSTOM_VIEW_SCREEN_NAME,
            activeScenario.ownerName,
        ) {
            JankHunterTelemetry.traceOperation("$CUSTOM_VIEW_OPERATION_NAME.${activeScenario.stepName}", block = block)
        }
    }

    private fun drawReferenceGrid(canvas: Canvas) {
        val widthStep = width / GRID_COLUMNS.toFloat()
        val heightStep = height / GRID_ROWS.toFloat()
        repeat(GRID_COLUMNS + 1) { column ->
            canvas.drawLine(column * widthStep, 0f, column * widthStep, height.toFloat(), paint)
        }
        repeat(GRID_ROWS + 1) { row ->
            canvas.drawLine(0f, row * heightStep, width.toFloat(), row * heightStep, paint)
        }
    }

    private fun drawHeavyFrame(canvas: Canvas) {
        burnCpu(HEAVY_DRAW_WORK_MS)
        repeat(HEAVY_PATH_COUNT) { index ->
            val radius = (index + 1f) * width.coerceAtMost(height) / (HEAVY_PATH_COUNT * 2f)
            canvas.drawCircle(width / 2f, height / 2f, radius, paint)
        }
    }

    private fun drawAllocationHeavyFrame(canvas: Canvas) {
        repeat(ALLOCATED_PATH_COUNT) { pathIndex ->
            val path = Path()
            path.moveTo(0f, height / 2f)
            repeat(POINTS_PER_PATH) { pointIndex ->
                val x = pointIndex * width / POINTS_PER_PATH.toFloat()
                val wave = sin((pointIndex + pathIndex) * WAVE_STEP).toFloat()
                path.lineTo(x, height / 2f + wave * height / WAVE_HEIGHT_DIVISOR)
            }
            canvas.drawPath(path, paint)
        }
        burnCpu(ALLOCATION_WORK_MS)
    }

    private fun burnCpu(durationMs: Long) {
        val deadline = SystemClock.elapsedRealtimeNanos() + durationMs * NANOS_PER_MS
        var value = checksum + 1.0
        while (SystemClock.elapsedRealtimeNanos() < deadline) {
            repeat(CPU_BATCH_SIZE) { index ->
                value += sin(value + index) * cos(value - index)
            }
        }
        checksum = value
    }

    private companion object {
        const val STROKE_WIDTH_DP = 1.2f
        const val GRID_COLUMNS = 8
        const val GRID_ROWS = 5
        const val HEAVY_DRAW_WORK_MS = 115L
        const val ALLOCATION_WORK_MS = 42L
        const val LAYOUT_WORK_MS = 38L
        const val HEAVY_PATH_COUNT = 90
        const val ALLOCATED_PATH_COUNT = 36
        const val POINTS_PER_PATH = 96
        const val WAVE_STEP = 0.17
        const val WAVE_HEIGHT_DIVISOR = 2.8f
        const val CPU_BATCH_SIZE = 128
        const val NANOS_PER_MS = 1_000_000L
    }
}
