package io.jankhunter.sample.automatic

import android.os.SystemClock
import io.jankhunter.sample.graph.PerformanceScenarioUseCase
import io.jankhunter.runtime.JankHunter
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
        JankHunter.wrapExecutorService(
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
        delay(JVMTI_CONTENTION_AT_MS - SEVERE_STALL_AT_MS - SEVERE_STALL_MS)
        runJvmtiEvidence()
        delay(STAGE_DURATION_MS - JVMTI_CONTENTION_AT_MS - JVMTI_CONTENTION_MS - FLUSH_ADVANCE_MS)
        completeAutomaticStage(ScenarioStep.UI_CPU)
        delay(FLUSH_ADVANCE_MS)
    }

    override fun close() {
        executor.shutdownNow()
    }

    private fun runExecutorFlows() {
        executor.execute {
            JankHunter.withFlow("sample.auto.cpu.sustained") {
                JankHunter.markFlowStep("busy_1200_ms")
                val checksum = graphScenario.calculateFor(CPU_WORK_MS)
                JankHunter.recordGauge("sample.auto.cpu.checksum", checksum)
            }
        }
        repeat(QUEUE_TASK_COUNT) { index ->
            executor.execute {
                JankHunter.withFlow("sample.auto.executor.queued") {
                    JankHunter.markFlowStep("queued_task_$index")
                    SystemClock.sleep(QUEUE_TASK_MS)
                    JankHunter.recordCounter("sample.auto.executor.task.completed.count", 1)
                }
            }
        }
    }

    private fun runMainThreadStall(flow: String, step: String, durationMs: Long) {
        JankHunter.withFlow(flow) {
            JankHunter.markFlowStep(step)
            graphScenario.renderFor(durationMs)
            JankHunter.recordCounter("sample.auto.ui.stall.completed.count", 1)
        }
    }

    private fun runJvmtiEvidence() {
        JankHunter.withFlow("sample.auto.jvmti.monitor_contention") {
            JankHunter.markFlowStep("wait_420_ms_with_gc")
            val result = graphScenario.collectJvmtiEvidence(JVMTI_CONTENTION_MS)
            JankHunter.recordCounter("sample.auto.jvmti.contention.completed.count", 1)
            JankHunter.recordGauge("sample.auto.jvmti.contention.wait_ms", result.mainThreadWaitMs)
            JankHunter.recordGauge("sample.auto.jvmti.allocation_bytes", result.allocatedBytes)
        }
    }

    private companion object {
        const val MODERATE_STALL_AT_MS = 450L
        const val SEVERE_STALL_AT_MS = 1_350L
        const val JVMTI_CONTENTION_AT_MS = 2_350L
        const val MODERATE_STALL_MS = 180L
        const val SEVERE_STALL_MS = 520L
        const val JVMTI_CONTENTION_MS = 420L
        const val CPU_WORK_MS = 1_200L
        const val QUEUE_TASK_COUNT = 3
        const val QUEUE_TASK_MS = 90L
        const val STAGE_DURATION_MS = 4_200L
        const val FLUSH_ADVANCE_MS = 250L
    }
}
