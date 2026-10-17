package io.jankhunter.runtime.internal.system

import java.lang.ref.WeakReference
import io.jankhunter.runtime.observedThreadId
import io.jankhunter.runtime.observedThreadState
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookFailureTracker
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/** Java observations remain separate from kernel state and are not creation events. */
internal data class JavaThreadObservation(
    val id: Long,
    val name: String,
    val state: Thread.State,
    val daemon: Boolean,
    val firstObservedElapsedNanos: Long,
    val source: String,
    val ownedByJankHunter: Boolean = false,
)

/** Owned by one collection epoch. Only the executing thread may register its Linux TID. */
internal class ThreadIdentityRegistry(
    private val limit: Int = 2048,
    private val weak: (Thread) -> WeakReference<Thread> = { WeakReference(it) },
) {
    private data class Entry(val thread: WeakReference<Thread>, val observed: Long, val source: String, val owned: Boolean)
    private val lock = ReentrantLock()
    private val entries = HashMap<Long, Entry>()

    private val rejected = AtomicLong()
    val dropped: Long get() = rejected.get().let { if (it < 0L) Long.MAX_VALUE else it }

    init { require(limit in 1..2048) }

    fun register(tid: Long, thread: Thread, observedNanos: Long, source: String, ownedByJankHunter: Boolean = false): Boolean {
        if (tid <= 0 || observedNanos < 0) return false
        if (!lock.tryLock()) {
            rejected.incrementAndGet()
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.REGISTRY_CONTENTION)
            return false
        }
        try {
            val previous = entries[tid]
            if (previous?.thread?.get() === thread && (!ownedByJankHunter || previous.owned)) return true
            if (previous == null && entries.size >= limit) {
                rejected.incrementAndGet()
                return false
            }
            entries[tid] = Entry(weak(thread), observedNanos, source.take(256), ownedByJankHunter)
            return true
        } finally { lock.unlock() }
    }

    /** Reject registrations after scan start: they might describe a reused TID after a stat read. */
    fun observe(tid: Long, scanStartedNanos: Long): JavaThreadObservation? {
        val entry = lock.withLock { entries[tid] } ?: return null
        if (entry.observed > scanStartedNanos) return null
        val thread = entry.thread.get()?.takeIf(Thread::isAlive) ?: return null
        return RuntimeHookGuard.value(null) {
            val id = observedThreadId(thread)
            val state = observedThreadState(thread) ?: return@value null
            if (id <= 0) return@value null
            val observation = JavaThreadObservation(
                id, thread.name.take(256), state, thread.isDaemon, entry.observed, entry.source, entry.owned,
            )
            // The strong local is used only during this observation, never retained by a report.
            observation.takeIf { thread.isAlive }
        }
    }

    /** Maintenance only; no sweeping or /proc I/O occurs in the registration hook. */
    fun prune() = lock.withLock {
        entries.entries.removeAll { it.value.thread.get()?.isAlive != true }
    }
}
