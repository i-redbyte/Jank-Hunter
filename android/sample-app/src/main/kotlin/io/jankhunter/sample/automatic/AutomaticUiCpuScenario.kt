package io.jankhunter.sample.automatic

import io.jankhunter.runtime.JankHunterTelemetry

import android.os.SystemClock
import io.jankhunter.sample.graph.PerformanceScenarioUseCase
import java.util.concurrent.Executors
import kotlinx.coroutines.delay

internal class AutomaticUiCpuScenario(
    private val graphScenario: PerformanceScenarioUseCase,
) : AutomaticScenarioStage {
    override val step = ScenarioStep.UI_CPU
    private val executorDelegate = Executors.newFixedThreadPool(1) { runnable ->
        Thread(runnable, "SampleCpuWorker")
    }
    private val executor by lazy {
        JankHunterTelemetry.wrapExecutorService(
            executorDelegate,
            "sample_cpu_queue",
            "sample.auto.cpu.executor",
        ) ?: executorDelegate
    }

    override suspend fun execute(context: AutomaticStageContext) {
        runExecutorFlows()
        delay(MODERATE_STALL_AT_MS)
        runMainThreadStall("sample.auto.ui.moderate_stall", "block_180_ms", MODERATE_STALL_MS)
        delay(SEVERE_STALL_AT_MS - MODERATE_STALL_AT_MS - MODERATE_STALL_MS)
        runMainThreadStall("sample.auto.ui.severe_stall", "block_520_ms", SEVERE_STALL_MS)
        delay(STAGE_DURATION_MS - SEVERE_STALL_AT_MS - SEVERE_STALL_MS - FLUSH_ADVANCE_MS)
        completeAutomaticStage(ScenarioStep.UI_CPU)
        delay(FLUSH_ADVANCE_MS)
    }

    override fun close() {
        executor.shutdownNow()
    }

    private fun runExecutorFlows() {
        executor.execute {
            JankHunterTelemetry.traceOperation("sample.auto.cpu.busy_1200_ms") {
                val checksum = graphScenario.calculateFor(CPU_WORK_MS)
                JankHunterTelemetry.gauge("sample.auto.cpu.checksum", checksum)
            }
        }
        repeat(QUEUE_TASK_COUNT) { index ->
            executor.execute {
                JankHunterTelemetry.traceOperation("sample.auto.executor.queued_task_$index") {
                    SystemClock.sleep(QUEUE_TASK_MS)
                    JankHunterTelemetry.counter("sample.auto.executor.task.completed.count", 1)
                }
            }
        }
    }

    private fun runMainThreadStall(operation: String, stage: String, durationMs: Long) {
        JankHunterTelemetry.traceOperation("$operation.$stage") {
            graphScenario.renderFor(durationMs)
            JankHunterTelemetry.counter("sample.auto.ui.stall.completed.count", 1)
        }
    }

    private companion object {
        const val MODERATE_STALL_AT_MS = 450L
        const val SEVERE_STALL_AT_MS = 1_350L
        const val MODERATE_STALL_MS = 180L
        const val SEVERE_STALL_MS = 520L
        const val CPU_WORK_MS = 1_200L
        const val QUEUE_TASK_COUNT = 3
        const val QUEUE_TASK_MS = 90L
        const val STAGE_DURATION_MS = 3_600L
        const val FLUSH_ADVANCE_MS = 250L
    }
}
