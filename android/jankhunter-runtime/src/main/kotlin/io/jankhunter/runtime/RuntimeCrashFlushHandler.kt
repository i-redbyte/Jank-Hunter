package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicBoolean

/** Bounded drain ordering; every stage must honor the supplied remaining timeout. */
internal class RuntimeCrashFlushHandler(
    private val previous: Thread.UncaughtExceptionHandler?,
    private val diagnostic: (String) -> Unit,
    private val metrics: (Long) -> Boolean,
    private val hooks: (Long) -> Boolean,
    private val graph: (Long) -> Boolean,
    private val writer: (Long) -> Boolean,
    private val nanoTime: RuntimeLongSource = RuntimeLongSource(System::nanoTime),
    private val draining: AtomicBoolean = AtomicBoolean(),
    private val requestWriterFlush: () -> Unit = {},
) : Thread.UncaughtExceptionHandler {
    override fun uncaughtException(thread: Thread, throwable: Throwable) {
        try {
            val startedAtNs = nanoTime.getAsLong()
            record("jankhunter.runtime.crash.count")
            if (!draining.compareAndSet(false, true)) {
                recordIncomplete("concurrent")
                requestFlush()
                return
            }
            try {
                drain("metrics", metrics, startedAtNs)
                drain("hooks", hooks, startedAtNs)
                drain("graph", graph, startedAtNs)
                if (!drain("writer", writer, startedAtNs)) requestFlush()
            } finally {
                draining.set(false)
            }
        } finally {
            previous?.uncaughtException(thread, throwable) ?: throw throwable
        }
    }

    private fun drain(name: String, stage: (Long) -> Boolean, startedAtNs: Long): Boolean {
        val elapsedNs = (nanoTime.getAsLong() - startedAtNs).coerceAtLeast(0L)
        val remainingMs = (CRASH_BUDGET_NS - elapsedNs).coerceAtLeast(0L) / NANOS_PER_MS
        if (remainingMs == 0L) {
            recordIncomplete(name)
            return false
        }
        val complete = try {
            stage(remainingMs)
        } catch (failure: Throwable) {
            recordIncomplete(name)
            RuntimeHookGuard.rethrowFatal(failure)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            return false
        }
        if (!complete) recordIncomplete(name)
        return complete
    }

    private fun requestFlush() = RuntimeHookGuard.run(RuntimeHookFailureReason.RUNTIME_LIFECYCLE) {
        requestWriterFlush()
    }

    private fun recordIncomplete(name: String) = record("jankhunter.runtime.crash_flush.incomplete.$name.count")

    private fun record(name: String) = RuntimeHookGuard.run(RuntimeHookFailureReason.RUNTIME_LIFECYCLE) {
        diagnostic(name)
    }

    private companion object {
        const val NANOS_PER_MS = 1_000_000L
        const val CRASH_BUDGET_NS = 100L * NANOS_PER_MS
    }
}
