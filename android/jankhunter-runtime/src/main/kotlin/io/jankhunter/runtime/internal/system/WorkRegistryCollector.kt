package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterWorkRegistryUnavailable
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.JankHunterWorkRegistrySource
import io.jankhunter.runtime.RuntimeWorkCleanup
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/** One in-flight request; CAS admission prevents cancelled or previous-generation callbacks. */
internal class WorkRegistryCollector(
    private val source: JankHunterWorkRegistrySource,
    private val executor: Executor,
    private val clock: () -> Long,
    private val cleanup: RuntimeWorkCleanup,
    private val timeoutMs: Long = 60_000,
    private val report: (JankHunterWorkRegistryResult, Long, Long) -> Unit,
) {
    private val active = AtomicBoolean()
    private val generation = AtomicLong()
    private val current = AtomicReference<Request?>()

    fun activate() {
        generation.incrementAndGet()
        active.set(true)
    }

    fun stop() {
        active.set(false)
        generation.incrementAndGet()
        current.getAndSet(null)?.close()
    }

    fun sampleOnce() {
        if (!active.get()) return
        val now = clock()
        current.get()?.let { pending ->
            if (now - pending.started >= timeoutMs && current.compareAndSet(pending, null)) {
                pending.close()
                if (active.get() && generation.get() == pending.generation) {
                    report(JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_TIMEOUT), pending.started, now)
                }
            }
            return
        }
        val request = Request(now, generation.get(), cleanup)
        request.bind { result ->
            if (current.compareAndSet(request, null)) {
                request.close()
                if (active.get() && generation.get() == request.generation) report(result, request.started, clock())
            }
        }
        if (!current.compareAndSet(null, request)) return
        if (!active.get() || generation.get() != request.generation) {
            current.compareAndSet(request, null)
            request.close()
            return
        }
        try {
            request.attach(source.request(executor, request::complete))
        } catch (_: Exception) {
            if (current.compareAndSet(request, null)) {
                request.close()
                if (active.get() && generation.get() == request.generation) {
                    report(JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_FAILED), request.started, clock())
                }
            }
        }
    }

    private class Request(val started: Long, val generation: Long, private val cleanup: RuntimeWorkCleanup) : AutoCloseable {
        private val closed = AtomicBoolean()
        private val handle = AtomicReference<AutoCloseable?>()
        private val publication = AtomicReference<((JankHunterWorkRegistryResult) -> Unit)?>(null)

        fun bind(callback: (JankHunterWorkRegistryResult) -> Unit) { publication.set(callback) }

        fun complete(result: JankHunterWorkRegistryResult) { publication.getAndSet(null)?.invoke(result) }

        fun attach(value: AutoCloseable) {
            if (closed.get()) { closeLater(value); return }
            handle.set(value)
            if (closed.get()) handle.getAndSet(null)?.let(::closeLater)
        }

        override fun close() {
            closed.set(true)
            // A foreign source may retain its callback after cancellation. Detach the collector
            // and its writer now, without waiting for the adapter's close implementation.
            publication.set(null)
            handle.getAndSet(null)?.let(::closeLater)
        }

        private fun closeLater(value: AutoCloseable) {
            cleanup.execute(value::close)
        }
    }
}
