package io.jankhunter.sample.manual

import io.jankhunter.sample.LeakCanaryBridge
import io.jankhunter.sample.R
import io.jankhunter.sample.SampleApplication
import io.jankhunter.runtime.JankHunter

internal class JankHunterManualScenarioRunner(
    private val application: SampleApplication,
    private val stateSink: ManualStateSink,
) : ManualScenarioRunner {
    private val text = SampleText(application)
    private val runtime = ManualRuntimeScenarios(text, stateSink)
    private val performance = ManualPerformanceScenarios(application, text, stateSink)
    private val retention = ManualRetentionScenarios(application, text, stateSink)
    private val handlers = mergeHandlers(
        runtime.handlers,
        performance.handlers,
        retention.handlers,
        mapOf(
            ManualAction.RUN_CLEAN_BASELINE to { runCleanBaseline() },
            ManualAction.RUN_NOISY_CANDIDATE to { runNoisyCandidate(requireNotNull(it)) },
            ManualAction.FLUSH_DIAGNOSTICS to { flushDiagnostics() },
            ManualAction.SHARE_DIAGNOSTICS to { stateSink.emit(ManualStateUpdate.ShareDiagnostics) },
            ManualAction.OPEN_CUSTOM_VIEW_LAB to { stateSink.emit(ManualStateUpdate.OpenCustomViewLab) },
            ManualAction.OPEN_COMPOSE_LAB to { stateSink.emit(ManualStateUpdate.OpenComposeLab) },
            ManualAction.BOTH_CLEAN_OBJECT to { runCleanLeakCanaryBenchmark() },
            ManualAction.BOTH_RETAINED_OBJECT to { runRetainedLeakCanaryBenchmark(requireNotNull(it)) },
            ManualAction.BOTH_CACHE_BURST to { runCacheLeakCanaryBenchmark() },
            ManualAction.HOW_TO_COMPARE to { showComparisonHint() },
            ManualAction.CANDIDATE_PERF_BURST to { runCandidatePerformanceBurst() },
            ManualAction.PULL_REPORT_HINT to { showPullReportHint() },
        ),
    )

    override fun initialize() {
        val ready = text(R.string.status_ready)
        runtime.refresh(ready)
        refreshLeakCanary(ready)
    }

    override fun execute(action: ManualAction, activityReference: Any?) {
        handlers.getValue(action)(activityReference)
    }

    override fun close() {
        performance.close()
    }

    private fun runCleanBaseline() {
        application.resetScenario()
        JankHunter.withFlow("sample.guided.baseline") {
            JankHunter.markFlowStep("clean_probe")
            performance.recordCustomMetrics()
            retention.recordCleanObject()
        }
        JankHunter.flush()
        status(text(R.string.status_baseline_recorded))
    }

    private fun runNoisyCandidate(activityReference: Any) {
        JankHunter.withFlow("sample.guided.candidate") {
            JankHunter.markFlowStep("regression_pack")
            performance.recordUiStall()
            performance.recordMemoryPressure()
            retention.recordCacheEntries()
            retention.recordLeakRegressionBurst(activityReference)
        }
        JankHunter.flush()
        status(text(R.string.status_candidate_recorded))
    }

    private fun runCandidatePerformanceBurst() {
        repeat(2) { performance.recordUiStall() }
        performance.recordMemoryPressure()
        performance.recordLogSpamBurst()
        status(text(R.string.status_candidate_perf_burst_recorded))
    }

    private fun runCleanLeakCanaryBenchmark() {
        retention.recordCleanObject()
        refreshLeakCanary(text(R.string.status_clean_object_queued))
    }

    private fun runRetainedLeakCanaryBenchmark(activityReference: Any) {
        retention.recordLeakCanaryObject(activityReference)
        refreshLeakCanary(text(R.string.status_retained_object_queued))
    }

    private fun runCacheLeakCanaryBenchmark() {
        retention.recordCacheEntries()
        refreshLeakCanary(text(R.string.status_cache_burst_queued))
    }

    private fun flushDiagnostics() {
        JankHunter.flush()
        status(text(R.string.status_flushed))
    }

    private fun showComparisonHint() {
        JankHunter.flush()
        status(text(R.string.status_compare_hint))
        refreshLeakCanary(text(R.string.status_comparison_hint_shown))
    }

    private fun showPullReportHint() {
        JankHunter.flush()
        status(text(R.string.status_pull_report_hint))
    }

    private fun refreshLeakCanary(reason: String) {
        stateSink.emit(
            ManualStateUpdate.LeakCanary(
                text(
                    R.string.leakcanary_status_line,
                    LeakCanaryBridge.status(application),
                    reason,
                ),
            ),
        )
    }

    private fun status(value: String) {
        stateSink.emit(ManualStateUpdate.Status(value))
    }

    private fun mergeHandlers(
        vararg sources: Map<ManualAction, ManualActionHandler>,
    ): Map<ManualAction, ManualActionHandler> {
        val entries = sources.flatMap { it.entries }
        val actions = entries.map { it.key }
        require(actions.size == actions.distinct().size) { "Manual action handlers must be unique" }
        require(actions.toSet() == ManualAction.entries.toSet()) { "Every manual action must have a handler" }
        return entries.associate { it.key to it.value }
    }
}
