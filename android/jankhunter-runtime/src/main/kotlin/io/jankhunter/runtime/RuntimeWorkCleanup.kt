package io.jankhunter.runtime

/** Runtime-owned cancellation lane shared across observation epochs and registry requests. */
internal class RuntimeWorkCleanup(
    private val executor: RuntimeTaskExecutor = RuntimeMetricDrainExecutor(
        "JankHunterWorkCancellation", "Jank Hunter work cancellation", queueCapacity = 64,
    ),
) {
    fun execute(action: () -> Unit) = RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR) {
        if (!executor.execute {
                RuntimeHookGuard.run(RuntimeHookFailureReason.COLLECTOR, action)
            }) RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
    }
}
