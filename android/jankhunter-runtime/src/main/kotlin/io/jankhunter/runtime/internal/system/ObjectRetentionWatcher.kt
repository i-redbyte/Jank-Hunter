package io.jankhunter.runtime.internal.system

import android.os.SystemClock
import io.jankhunter.runtime.JankHunterContext
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeLongConsumer
import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.internal.monotonicDeadlineAfterMillis
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
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

internal typealias HeapDumpReporter = (
    className: String?,
    ownerHint: String?,
    context: JankHunterContext?,
    ageMs: Long,
    count: Long,
) -> Unit

internal class ObjectRetentionWatcher(
    retainedDelayMs: Long,
    private val forceGcBeforeReport: Boolean = false,
    private val clock: RuntimeLongSource = RuntimeLongSource { SystemClock.elapsedRealtime() },
    private val requestGc: () -> Unit = {
        Runtime.getRuntime().gc()
        System.runFinalization()
    },
    private val reporter: RetentionReporter = NO_OP_REPORTER,
    maxWatchedReferences: Int = DEFAULT_MAX_WATCHED_REFERENCES,
    private val exactAdmission: Boolean = false,
    private val onCardinalityLoss: RuntimeLongConsumer = NO_OP_CARDINALITY_LOSS,
    heapDumpMinRetainedAgeMs: Long = 0L,
    private val heapDumpReporter: HeapDumpReporter? = null,
) {
    private val delayMs = max(1_000L, retainedDelayMs)
    private val checkIntervalMs = max(500L, min(delayMs / 2L, 2_000L))
    private val running = AtomicBoolean(false)
    private val queue = ReferenceQueue<Any>()
    private val capacity = maxWatchedReferences.coerceAtLeast(0)
    private val watched = ArrayList<WatchedReference>()
    private val watchedByIdentityHash = HashMap<Int, WatchedReference>()
    private val registryLock = Any()
    private val checkLock = ReentrantLock()
    private val lifecycleLock = Any()
    private val heapDumpAgeMs = max(delayMs, heapDumpMinRetainedAgeMs.coerceAtLeast(0L))
    private var maintenance: MaintenanceHandle? = null

    @Volatile
    private var finalCheck: FinalCheck? = null

    fun start(scheduler: RuntimeMaintenanceScheduler) {
        synchronized(lifecycleLock) {
            if (running.get() || finalCheck?.completed?.count == 1L || !checkLock.tryLock()) return
            try {
                finalCheck = null
                running.set(true)
                maintenance = scheduler.schedule(delayMs = { checkIntervalMs }) { checkRetained() }
            } finally {
                checkLock.unlock()
            }
        }
    }

    fun stop(timeoutMs: Long = DEFAULT_STOP_TIMEOUT_MS): StopResult {
        val deadlineNs = monotonicDeadlineAfterMillis(timeoutMs)
        var launch = false
        val final = synchronized(lifecycleLock) {
            if (!running.getAndSet(false)) return@synchronized finalCheck
            maintenance?.cancel()
            maintenance = null
            launch = true
            FinalCheck(deadlineNs).also { finalCheck = it }
        } ?: return StopResult.COMPLETED
        // The platform cannot interrupt an HPROF dump. This worker owns final diagnostics even
        // after the caller's deadline, and never holds the lifecycle lock while doing that work.
        if (launch) {
            try {
                Thread({ finish(final) }, "JankHunterRetentionStop").apply {
                    isDaemon = true
                    priority = Thread.MIN_PRIORITY
                    start()
                }
            } catch (failure: Throwable) {
                final.result = StopResult.FAILED
                complete(final)
                RuntimeHookGuard.rethrowFatal(failure)
            }
        }
        return final.await(deadlineNs)
    }

    private fun finish(final: FinalCheck) {
        try {
            val remainingNs = (final.deadlineNs - System.nanoTime()).coerceAtLeast(0L)
            if (!checkLock.tryLock(remainingNs, TimeUnit.NANOSECONDS)) return
            try {
                if (exactAdmission && canDiagnose()) {
                    checkRetainedLocked()
                    // Seal force-GC survivors without waiting for the cancelled periodic task.
                    checkRetainedLocked()
                }
                final.result = when {
                    !exactAdmission -> StopResult.SKIPPED
                    final.deadlineNs - System.nanoTime() <= 0L -> StopResult.TIMED_OUT
                    else -> StopResult.COMPLETED
                }
            } finally {
                checkLock.unlock()
            }
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            final.result = StopResult.INTERRUPTED
        } catch (failure: Throwable) {
            final.result = StopResult.FAILED
            RuntimeHookGuard.rethrowFatal(failure)
        } finally {
            complete(final)
        }
    }

    private fun complete(final: FinalCheck) {
        try {
            synchronized(registryLock) {
                watched.forEach(WatchedReference::retire)
                watched.clear()
                watchedByIdentityHash.clear()
                while (queue.poll() != null) {
                    // Do not retain stale references between runtime sessions.
                }
            }
        } finally {
            final.completed.countDown()
        }
    }

    private fun canDiagnose(): Boolean = running.get() ||
        (exactAdmission && finalCheck?.let { it.deadlineNs - System.nanoTime() > 0L } == true)

    fun watch(instance: Any?, description: String?, ownerHint: String?, context: JankHunterContext?) {
        if (instance == null || !running.get()) return
        addWatched(instance, description, ownerHint, context)
    }

    private fun addWatched(instance: Any, description: String?, ownerHint: String?, context: JankHunterContext?) {
        var dropped = false
        synchronized(registryLock) {
            drainClearedLocked()
            if (!running.get() || isAlreadyWatchedLocked(instance)) return
            if (watched.size >= capacity) {
                dropped = true
            } else {
                addWatchedLocked(
                    WatchedReference(
                        referent = instance,
                        queue = queue,
                        className = safeClassName(instance, description),
                        ownerHint = ownerHint?.takeIf { it.isNotBlank() },
                        context = context,
                        watchStartedMs = clock.getAsLong(),
                        identityHash = System.identityHashCode(instance),
                    ),
                )
            }
        }
        if (dropped) recordCardinalityLoss()
    }

    private fun isAlreadyWatchedLocked(instance: Any): Boolean {
        var candidate = watchedByIdentityHash[System.identityHashCode(instance)]
        while (candidate != null) {
            if (candidate.get() === instance) return true
            candidate = candidate.identityHashNext
        }
        return false
    }

    private fun addWatchedLocked(ref: WatchedReference) {
        ref.watchedIndex = watched.size
        ref.identityHashNext = watchedByIdentityHash.put(ref.identityHash, ref)
        watched.add(ref)
    }

    private fun drainClearedLocked() {
        while (true) {
            val ref = queue.poll() as? WatchedReference ?: return
            removeWatchedLocked(ref)
        }
    }

    internal fun checkRetained() {
        checkLock.withLock {
            checkRetainedLocked()
        }
    }

    private fun checkRetainedLocked() {
        if (!canDiagnose()) return
        val now = clock.getAsLong()
        var retainedGroups: LinkedHashMap<String, LinkedHashMap<String?, RetainedGroup>>? = null
        var heapDumpGroups: LinkedHashMap<String, LinkedHashMap<String?, RetainedGroup>>? = null
        var shouldRequestGc = false

        synchronized(registryLock) {
            drainClearedLocked()
            var index = 0
            while (index < watched.size) {
                val ref = watched[index]
                if (ref.get() == null) {
                    removeWatchedLocked(ref)
                    continue
                }

                val ageMs = now - ref.watchStartedMs
                if (ageMs < delayMs) {
                    index++
                    continue
                }

                if (ref.firstRetainedAtMs == 0L) {
                    ref.firstRetainedAtMs = now
                    if (forceGcBeforeReport && !ref.gcRequested) {
                        ref.gcRequested = true
                        shouldRequestGc = true
                        index++
                        continue
                    }
                }

                if (!ref.retentionReported) {
                    retainedGroups = addToGroups(retainedGroups, ref, ageMs, ref.evidence())
                    ref.retentionReported = true
                }

                if (heapDumpReporter != null && ageMs >= heapDumpAgeMs) {
                    heapDumpGroups = addToGroups(heapDumpGroups, ref, ageMs, null)
                    removeWatchedLocked(ref)
                } else if (heapDumpReporter == null) {
                    removeWatchedLocked(ref)
                } else {
                    index++
                }
            }
        }

        if (shouldRequestGc && canDiagnose()) {
            val completed = RuntimeHookGuard.value(false, RuntimeHookFailureReason.COLLECTOR) {
                requestGc()
                true
            }
            synchronized(registryLock) {
                for (ref in watched) {
                    if (ref.gcRequested && ref.firstRetainedAtMs != 0L) {
                        ref.gcCompleted = completed
                    }
                }
            }
        }
        if (!canDiagnose()) return
        retainedGroups?.reportTo(reporter)
        val dumpReporter = heapDumpReporter ?: return
        heapDumpGroups?.reportTo(dumpReporter)
    }

    private fun removeWatchedLocked(ref: WatchedReference) {
        val index = ref.watchedIndex
        if (index < 0) return
        val lastIndex = watched.lastIndex
        val last = watched.removeAt(lastIndex)
        if (index < lastIndex) {
            watched[index] = last
            last.watchedIndex = index
        }
        removeFromIdentityIndexLocked(ref)
        ref.retire()
    }

    private fun removeFromIdentityIndexLocked(ref: WatchedReference) {
        var previous: WatchedReference? = null
        var candidate = watchedByIdentityHash[ref.identityHash]
        while (candidate != null) {
            if (candidate === ref) {
                if (previous == null) {
                    val next = candidate.identityHashNext
                    if (next == null) watchedByIdentityHash.remove(ref.identityHash)
                    else watchedByIdentityHash[ref.identityHash] = next
                } else {
                    previous.identityHashNext = candidate.identityHashNext
                }
                candidate.identityHashNext = null
                return
            }
            previous = candidate
            candidate = candidate.identityHashNext
        }
    }

    private fun addToGroups(
        groups: LinkedHashMap<String, LinkedHashMap<String?, RetainedGroup>>?,
        ref: WatchedReference,
        ageMs: Long,
        evidence: RetentionEvidence?,
    ): LinkedHashMap<String, LinkedHashMap<String?, RetainedGroup>> {
        val target = groups ?: linkedMapOf()
        val group = target.getOrPut(ref.className, ::linkedMapOf)
            .getOrPut(ref.ownerHint, ref::newGroup)
        if (evidence == null) group.add(ageMs) else group.add(ageMs, evidence)
        return target
    }

    private fun Map<String, Map<String?, RetainedGroup>>.reportTo(target: RetentionReporter) {
        for (ownerGroups in values) {
            for (group in ownerGroups.values) {
                if (!canDiagnose()) return
                target(group.className, group.ownerHint, group.context, group.maxAgeMs, group.count, group.evidence)
            }
        }
    }

    private fun Map<String, Map<String?, RetainedGroup>>.reportTo(target: HeapDumpReporter) {
        for (ownerGroups in values) {
            for (group in ownerGroups.values) {
                if (!canDiagnose()) return
                target(group.className, group.ownerHint, group.context, group.maxAgeMs, group.count)
            }
        }
    }

    private fun recordCardinalityLoss() {
        RuntimeHookGuard.run { onCardinalityLoss.accept(1L) }
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
            add(ageMs)
            if (observedEvidence == RetentionEvidence.TIME_ONLY) {
                evidence = RetentionEvidence.TIME_ONLY
            }
        }

        fun add(ageMs: Long) {
            count++
            if (ageMs > maxAgeMs) {
                maxAgeMs = ageMs
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
        val identityHash: Int,
    ) : WeakReference<Any>(referent, queue) {
        var watchedIndex = -1
        var identityHashNext: WatchedReference? = null
        var firstRetainedAtMs = 0L
        var gcRequested = false
        var gcCompleted = false
        var retentionReported = false

        fun newGroup(): RetainedGroup = RetainedGroup(className, ownerHint, context)

        fun retire() {
            watchedIndex = -1
            identityHashNext = null
            clear()
        }

        fun evidence(): RetentionEvidence {
            return if (gcCompleted) RetentionEvidence.AFTER_EXPLICIT_GC else RetentionEvidence.TIME_ONLY
        }
    }

    enum class StopResult(val counterName: String) {
        COMPLETED("completed"),
        TIMED_OUT("timed_out"),
        INTERRUPTED("interrupted"),
        SKIPPED("skipped"),
        FAILED("failed"),
    }

    private class FinalCheck(val deadlineNs: Long) {
        val completed = CountDownLatch(1)

        @Volatile
        var result = StopResult.TIMED_OUT

        fun await(callerDeadlineNs: Long): StopResult = try {
            val remainingNs = (callerDeadlineNs - System.nanoTime()).coerceAtLeast(0L)
            if (completed.await(remainingNs, TimeUnit.NANOSECONDS)) result else StopResult.TIMED_OUT
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            StopResult.INTERRUPTED
        }
    }

    private companion object {
        const val DEFAULT_STOP_TIMEOUT_MS = 5_000L
        const val DEFAULT_MAX_WATCHED_REFERENCES = 2_048
        val NO_OP_REPORTER: RetentionReporter = { _, _, _, _, _, _ -> }
        val NO_OP_CARDINALITY_LOSS = RuntimeLongConsumer { }
    }
}
