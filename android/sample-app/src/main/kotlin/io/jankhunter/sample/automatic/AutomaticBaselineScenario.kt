package io.jankhunter.sample.automatic

import io.jankhunter.runtime.JankHunterTelemetry

import android.os.SystemClock
import io.jankhunter.sample.ReleasedCheckoutProbe
import io.jankhunter.sample.graph.BaselineScenarioUseCase
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.Executors
import kotlinx.coroutines.delay

internal class AutomaticBaselineScenario(
    private val graphScenario: BaselineScenarioUseCase,
) : AutomaticScenarioStage {
    override val step = ScenarioStep.BASELINE
    private val executorDelegate = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "SampleBaselineWorker")
    }
    private val executor by lazy {
        JankHunterTelemetry.wrapExecutorService(
            executorDelegate,
            "sample_baseline",
            "sample.auto.baseline.executor",
        ) ?: executorDelegate
    }

    override suspend fun execute(context: AutomaticStageContext) {
        JankHunterTelemetry.traceOperation("sample.auto.baseline.record_expected_values") {
            JankHunterTelemetry.counter("sample.auto.baseline.operation.count", 3)
            JankHunterTelemetry.gauge("sample.auto.baseline.item_count", 24)
            JankHunterTelemetry.gauge("sample.auto.baseline.duration_budget_ms", 50)
        }
        JankHunterTelemetry.traceOperation("sample.auto.baseline.watch_without_retention") {
            val released = ReleasedCheckoutProbe()
            JankHunterTelemetry.watch(
                released,
                ReleasedCheckoutProbe::class.java.name,
                "sample.auto.baseline.released_object",
            )
        }
        executor.execute {
            JankHunterTelemetry.traceOperation("sample.auto.baseline.executor.quick_task") {
                SystemClock.sleep(20)
                val itemCount = graphScenario.execute()
                JankHunterTelemetry.gauge("sample.auto.baseline.graph_item_count", itemCount)
            }
        }
        delay(STAGE_DURATION_MS - FLUSH_ADVANCE_MS)
        completeAutomaticStage(ScenarioStep.BASELINE)
        delay(FLUSH_ADVANCE_MS)
    }

    override fun close() {
        executor.shutdownNow()
    }

    private companion object {
        const val STAGE_DURATION_MS = 2_800L
        const val FLUSH_ADVANCE_MS = 250L
    }
}
