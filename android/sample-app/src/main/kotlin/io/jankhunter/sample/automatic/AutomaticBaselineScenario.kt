package io.jankhunter.sample.automatic

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
        JankHunter.wrapExecutorService(
            executorDelegate,
            "sample_baseline",
            "sample.auto.baseline.executor",
        ) ?: executorDelegate
    }

    override suspend fun execute(context: AutomaticStageContext) {
        JankHunter.withFlow("sample.auto.baseline.custom_metrics") {
            JankHunter.markFlowStep("record_expected_values")
            JankHunter.recordCounter("sample.auto.baseline.operation.count", 3)
            JankHunter.recordGauge("sample.auto.baseline.item_count", 24)
            JankHunter.recordGauge("sample.auto.baseline.duration_budget_ms", 50)
        }
        JankHunter.withFlow("sample.auto.baseline.released_object") {
            JankHunter.markFlowStep("watch_without_retention")
            val released = ReleasedCheckoutProbe()
            JankHunter.watchObject(
                released,
                ReleasedCheckoutProbe::class.java.name,
                "sample.auto.baseline.released_object",
            )
        }
        executor.execute {
            JankHunter.withFlow("sample.auto.baseline.executor") {
                JankHunter.markFlowStep("quick_task")
                SystemClock.sleep(20)
                val itemCount = graphScenario.execute()
                JankHunter.recordGauge("sample.auto.baseline.graph_item_count", itemCount)
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
