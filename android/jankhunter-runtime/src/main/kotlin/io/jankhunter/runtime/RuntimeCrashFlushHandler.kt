package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Bounded drain ordering; every stage must honor the supplied remaining timeout. */
internal class RuntimeCrashFlushHandler(
    private val previous: Thread.UncaughtExceptionHandler?,
    diagnostic: (String) -> Unit,
    metrics: (Long) -> Boolean,
    hooks: (Long) -> Boolean,
    graph: (Long) -> Boolean,
    writer: (Long) -> Boolean,
    nanoTime: RuntimeLongSource = RuntimeLongSource(System::nanoTime),
    private val draining: AtomicBoolean = AtomicBoolean(),
    requestWriterFlush: () -> Unit = {},
) : Thread.UncaughtExceptionHandler {
    private val callbacks = AtomicReference(Callbacks(diagnostic, metrics, hooks, graph, writer, nanoTime, requestWriterFlush))

    /** A later host handler can retain this handler in its forwarding chain after SDK shutdown. */
    fun deactivate() { callbacks.set(null) }

    private class Callbacks(
        val diagnostic: (String) -> Unit,
        val metrics: (Long) -> Boolean,
        val hooks: (Long) -> Boolean,
        val graph: (Long) -> Boolean,
        val writer: (Long) -> Boolean,
        val nanoTime: RuntimeLongSource,
        val requestWriterFlush: () -> Unit,
    )

    override fun uncaughtException(thread: Thread, throwable: Throwable) {
        try {
            val active = callbacks.get() ?: return
            val startedAtNs = active.nanoTime.getAsLong()
            record(active, "jankhunter.runtime.crash.count")
            if (!draining.compareAndSet(false, true)) {
                recordIncomplete(active, "concurrent")
                requestFlush(active)
                return
            }
            try {
                drain(active, "metrics", active.metrics, startedAtNs)
                drain(active, "hooks", active.hooks, startedAtNs)
                drain(active, "graph", active.graph, startedAtNs)
                if (!drain(active, "writer", active.writer, startedAtNs)) requestFlush(active)
            } finally {
                draining.set(false)
            }
        } finally {
            previous?.uncaughtException(thread, throwable) ?: throw throwable
        }
    }

    private fun drain(active: Callbacks, name: String, stage: (Long) -> Boolean, startedAtNs: Long): Boolean {
        val elapsedNs = (active.nanoTime.getAsLong() - startedAtNs).coerceAtLeast(0L)
        val remainingMs = (CRASH_BUDGET_NS - elapsedNs).coerceAtLeast(0L) / NANOS_PER_MS
        if (remainingMs == 0L) {
            recordIncomplete(active, name)
            return false
        }
        val complete = try {
            stage(remainingMs)
        } catch (failure: Throwable) {
            recordIncomplete(active, name)
            RuntimeHookGuard.rethrowFatal(failure)
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.RUNTIME_LIFECYCLE)
            return false
        }
        if (!complete) recordIncomplete(active, name)
        return complete
    }

    private fun requestFlush(active: Callbacks) = RuntimeHookGuard.run(RuntimeHookFailureReason.RUNTIME_LIFECYCLE) {
        active.requestWriterFlush()
    }

    private fun recordIncomplete(active: Callbacks, name: String) = record(active, "jankhunter.runtime.crash_flush.incomplete.$name.count")

    private fun record(active: Callbacks, name: String) = RuntimeHookGuard.run(RuntimeHookFailureReason.RUNTIME_LIFECYCLE) {
        active.diagnostic(name)
    }

    private companion object {
        const val NANOS_PER_MS = 1_000_000L
        const val CRASH_BUDGET_NS = 100L * NANOS_PER_MS
    }
}
