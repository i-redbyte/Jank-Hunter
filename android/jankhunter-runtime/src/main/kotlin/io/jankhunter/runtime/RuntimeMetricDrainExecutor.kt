package io.jankhunter.runtime

import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit

/** A separate bounded lane lets a snapshot on the maintenance thread wait for a metric drain. */
internal class RuntimeMetricDrainExecutor : RuntimeTaskExecutor {
    private val executor = ThreadPoolExecutor(
        0, 1, IDLE_TIMEOUT_MS, TimeUnit.MILLISECONDS, ArrayBlockingQueue(1),
        { task -> Thread(task, "JankHunterMetricDrain").apply { isDaemon = true; priority = Thread.NORM_PRIORITY } },
        ThreadPoolExecutor.AbortPolicy(),
    )

    override fun execute(task: () -> Unit): Boolean = try {
        executor.execute { RuntimeHookGuard.run(RuntimeHookFailureReason.RUNTIME_LIFECYCLE, task) }
        true
    } catch (_: RejectedExecutionException) {
        false
    }

    private companion object {
        const val IDLE_TIMEOUT_MS = 1_000L
    }
}
