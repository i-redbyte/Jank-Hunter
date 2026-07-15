package io.jankhunter.runtime.internal.system

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterContext
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max
import kotlin.math.min

internal typealias RetentionReporter = (
    className: String?,
    ownerHint: String?,
    context: JankHunterContext?,
    ageMs: Long,
    count: Long,
    evidence: RetentionEvidence,
) -> Unit

internal class ObjectRetentionWatcher(
    retainedDelayMs: Long,
    private val forceGcBeforeReport: Boolean = false,
    private val clock: () -> Long = { SystemClock.elapsedRealtime() },
    private val requestGc: () -> Unit = {
        Runtime.getRuntime().gc()
        System.runFinalization()
    },
    private val reporter: RetentionReporter =
        { className, ownerHint, context, ageMs, count, evidence ->
            JankHunter.recordWatchedRetained(className, ownerHint, context, ageMs, count, evidence)
        },
    maxWatchedReferences: Int = DEFAULT_MAX_WATCHED_REFERENCES,
    private val onCardinalityLoss: (Long) -> Unit = { count ->
        JankHunter.recordQuality(QualityCounterId.OBJECT_WATCHER_LIMIT, count)
    },
    heapDumpMinRetainedAgeMs: Long = 0L,
    private val heapDumpReporter: RetentionReporter? = null,
) {
    private val delayMs = max(1_000L, retainedDelayMs)
    private val checkIntervalMs = max(500L, min(delayMs / 2L, 2_000L))
    private val running = AtomicBoolean(false)
    private val queue = ReferenceQueue<Any>()
    private val watched = ConcurrentLinkedQueue<WatchedReference>()
    private val registryLock = Any()
    private val capacity = maxWatchedReferences.coerceAtLeast(0)
    private val heapDumpAgeMs = max(delayMs, heapDumpMinRetainedAgeMs.coerceAtLeast(0L))
    private var watchedCount = 0
    private var maintenance: MaintenanceHandle? = null

    fun start(scheduler: RuntimeMaintenanceScheduler) {
        if (!running.compareAndSet(false, true)) return
        maintenance = scheduler.schedule(delayMs = { checkIntervalMs }) { checkRetained() }
    }

    fun stop() {
        running.set(false)
        maintenance?.cancel()
        maintenance = null
        synchronized(registryLock) {
            watched.clear()
            watchedCount = 0
            while (queue.poll() != null) {
                // Do not retain stale references between runtime sessions.
            }
        }
    }

    fun watch(instance: Any?, description: String?, ownerHint: String?, context: JankHunterContext?) {
        if (instance == null || !running.get()) return
        addWatched(instance, description, ownerHint, context)
    }

    private fun addWatched(instance: Any, description: String?, ownerHint: String?, context: JankHunterContext?) {
        var dropped = false
        synchronized(registryLock) {
            if (!running.get() || isAlreadyWatchedLocked(instance)) return
            if (watchedCount >= capacity) {
                dropped = true
            } else {
                watched.add(
                    WatchedReference(
                        instance,
                        queue,
                        safeClassName(instance, description),
                        ownerHint?.takeIf { it.isNotBlank() },
                        context,
                        clock(),
                    ),
                )
                watchedCount++
            }
        }
        if (dropped) recordCardinalityLoss()
    }

    private fun isAlreadyWatchedLocked(instance: Any): Boolean {
        for (ref in watched) {
            if (!ref.removed && ref.get() === instance) {
                return true
            }
        }
        return false
    }

    private fun drainCleared(): Boolean {
        var removed = false
        while (true) {
            val ref = queue.poll() as? WatchedReference ?: return removed
            ref.removed = true
            removed = true
        }
    }

    internal fun checkRetained() {
        if (!running.get()) return
        val now = clock()
        val retainedGroups = linkedMapOf<String, RetainedGroup>()
        val heapDumpGroups = linkedMapOf<String, RetainedGroup>()
        var shouldRequestGc = false
        var shouldCompact = drainCleared()

        for (ref in watched) {
            if (ref.removed) {
                shouldCompact = true
                continue
            }
            if (ref.get() == null) {
                ref.removed = true
                shouldCompact = true
                continue
            }

            val ageMs = now - ref.watchStartedMs
            if (ageMs < delayMs) continue

            if (ref.firstRetainedAtMs == 0L) {
                ref.firstRetainedAtMs = now
                if (forceGcBeforeReport && !ref.gcRequested) {
                    ref.gcRequested = true
                    shouldRequestGc = true
                    continue
                }
            }

            val key = ref.groupKey()
            if (!ref.retentionReported) {
                retainedGroups.getOrPut(key) { ref.newGroup() }
                    .add(ageMs, ref.evidence())
                ref.retentionReported = true
            }

            if (heapDumpReporter != null && ageMs >= heapDumpAgeMs) {
                heapDumpGroups.getOrPut(key) { ref.newGroup() }
                    .add(ageMs, ref.evidence())
                ref.removed = true
                shouldCompact = true
            } else if (heapDumpReporter == null) {
                ref.removed = true
                shouldCompact = true
            }
        }

        if (shouldCompact) {
            compactWatched()
        }

        if (shouldRequestGc && running.get()) {
            val completed = runCatching { requestGc() }.isSuccess
            for (ref in watched) {
                if (ref.gcRequested && ref.firstRetainedAtMs != 0L) {
                    ref.gcCompleted = completed
                }
            }
        }
        if (!running.get()) return
        for (group in retainedGroups.values) {
            reporter(group.className, group.ownerHint, group.context, group.maxAgeMs, group.count, group.evidence)
        }
        val dumpReporter = heapDumpReporter ?: return
        for (group in heapDumpGroups.values) {
            dumpReporter(group.className, group.ownerHint, group.context, group.maxAgeMs, group.count, group.evidence)
        }
    }

    private fun compactWatched() {
        synchronized(registryLock) {
            val survivors = ArrayList<WatchedReference>(watchedCount)
            while (true) {
                val ref = watched.poll() ?: break
                if (!ref.removed && ref.get() != null) {
                    survivors.add(ref)
                }
            }
            watchedCount = survivors.size
            survivors.forEach(watched::add)
        }
    }

    private fun recordCardinalityLoss() {
        try {
            onCardinalityLoss(1L)
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
        }
    }

    private fun safeClassName(instance: Any, description: String?): String {
        return description?.takeIf { it.isNotBlank() } ?: instance.javaClass.name
    }

    private class RetainedGroup(
        val className: String,
        val ownerHint: String?,
        val context: JankHunterContext?,
    ) {
        var count = 0L
            private set
        var maxAgeMs = 0L
            private set
        var evidence = RetentionEvidence.AFTER_EXPLICIT_GC
            private set

        fun add(ageMs: Long, observedEvidence: RetentionEvidence) {
            count++
            if (ageMs > maxAgeMs) {
                maxAgeMs = ageMs
            }
            if (observedEvidence == RetentionEvidence.TIME_ONLY) {
                evidence = RetentionEvidence.TIME_ONLY
            }
        }
    }

    private class WatchedReference(
        referent: Any,
        queue: ReferenceQueue<Any>,
        val className: String,
        val ownerHint: String?,
        val context: JankHunterContext?,
        val watchStartedMs: Long,
    ) : WeakReference<Any>(referent, queue) {
        var firstRetainedAtMs = 0L
        var gcRequested = false
        var gcCompleted = false
        var retentionReported = false
        @Volatile
        var removed = false

        fun groupKey(): String = className + "\u0000" + ownerHint.orEmpty()

        fun newGroup(): RetainedGroup = RetainedGroup(className, ownerHint, context)

        fun evidence(): RetentionEvidence {
            return if (gcCompleted) RetentionEvidence.AFTER_EXPLICIT_GC else RetentionEvidence.TIME_ONLY
        }
    }

    private companion object {
        const val DEFAULT_MAX_WATCHED_REFERENCES = 2_048
    }
}
