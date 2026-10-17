package io.jankhunter.runtime

import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit

/** A bounded diagnostics lane with no caller-runs fallback. */
internal class RuntimeMetricDrainExecutor(
    threadName: String = "JankHunterMetricDrain",
    private val threadLabel: String = "Jank Hunter metric drain",
    queueCapacity: Int = 1,
) : RuntimeTaskExecutor {
    private val executor = ThreadPoolExecutor(
        0, 1, IDLE_TIMEOUT_MS, TimeUnit.MILLISECONDS, ArrayBlockingQueue(queueCapacity),
        { task -> Thread(task, threadName).apply { isDaemon = true; priority = Thread.NORM_PRIORITY } },
        ThreadPoolExecutor.AbortPolicy(),
    )

    override fun execute(task: () -> Unit): Boolean = try {
        executor.execute {
            JankHunterHooks.observeOwnedThread(threadLabel)
            RuntimeHookGuard.run(RuntimeHookFailureReason.RUNTIME_LIFECYCLE, task)
        }
        true
    } catch (_: RejectedExecutionException) {
        false
    }

    private companion object {
        const val IDLE_TIMEOUT_MS = 1_000L
    }
}
