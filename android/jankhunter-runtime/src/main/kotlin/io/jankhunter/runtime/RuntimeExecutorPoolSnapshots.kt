package io.jankhunter.runtime

import java.lang.ref.WeakReference
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.locks.ReentrantLock

/** Native pool getters take the host's mainLock and may dispatch arbitrary overrides. */
internal class RuntimeExecutorPoolSnapshots(
    private val clock: RuntimeLongSource,
    private val execute: RuntimeTaskExecutor = RuntimeMetricDrainExecutor(
        "JankHunterPoolSnapshot", "Jank Hunter pool snapshot",
    ),
    capacity: Int = 32,
) {
    private val entries: Array<Entry?> = arrayOfNulls(capacity)
    private val lock = ReentrantLock()
    private var replacement = 0

    init { require(capacity > 0) }

    fun getOrRequest(executor: Any): Snapshot? {
        val pool = executor as? ThreadPoolExecutor ?: return null
        val entry = entry(pool) ?: return null
        val now = clock.getAsLong()
        val previous = entry.snapshot
        if ((!entry.requested || now < entry.requestedAt || now - entry.requestedAt >= INTERVAL_MS) &&
            entry.pending.compareAndSet(false, true)) {
            val requestedAt = clock.getAsLong()
            if (entry.requested && requestedAt >= entry.requestedAt && requestedAt - entry.requestedAt < INTERVAL_MS) {
                entry.pending.set(false)
                return previous?.takeIf { now >= it.atMs && now - it.atMs <= MAX_AGE_MS }
            }
            entry.requestedAt = requestedAt
            entry.requested = true
            try {
                if (!execute.execute { sample(entry) }) entry.pending.set(false)
            } catch (failure: Throwable) {
                entry.pending.set(false)
                RuntimeHookGuard.rethrowFatal(failure)
                RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
            }
        }
        return previous?.takeIf { now >= it.atMs && now - it.atMs <= MAX_AGE_MS }
    }

    private fun entry(pool: ThreadPoolExecutor): Entry? {
        if (!lock.tryLock()) return null
        try {
            var vacant = -1
            for (index in entries.indices) {
                val entry = entries[index]
                val owner = entry?.pool?.get()
                if (owner === pool) return entry
                if (owner == null && vacant < 0) vacant = index
            }
            val target = if (vacant >= 0) vacant else replacement
            return Entry(pool).also {
                entries[target] = it
                replacement = (target + 1) % entries.size
            }
        } finally { lock.unlock() }
    }

    private fun sample(entry: Entry) {
        try {
            val pool = entry.pool.get() ?: return
            // Only this dedicated bounded lane can wait for host locks. Never run on the
            // application, writer, metric-drain, or shared maintenance thread.
            val sampledAt = clock.getAsLong()
            val active = pool.activeCount
            val size = pool.poolSize
            val completed = pool.completedTaskCount
            entry.snapshot = Snapshot(active, size, completed, sampledAt)
        } finally { entry.pending.set(false) }
    }

    internal class Snapshot(val active: Int, val size: Int, val completed: Long, val atMs: Long)

    private class Entry(pool: ThreadPoolExecutor) {
        val pool = WeakReference(pool)
        val pending = AtomicBoolean()
        @Volatile var requested = false
        @Volatile var requestedAt = 0L
        @Volatile var snapshot: Snapshot? = null
    }

    private companion object {
        const val INTERVAL_MS = 1_000L
        const val MAX_AGE_MS = 2_000L
    }
}
