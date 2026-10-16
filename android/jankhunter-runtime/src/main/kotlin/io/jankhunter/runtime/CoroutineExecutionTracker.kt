package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

internal enum class CoroutineExecutionOutcome {
    SUCCESS,
    FAILURE,
    CANCELLED,
}

internal fun interface CoroutineExecutionSink {
    fun complete(
        owner: String,
        activeDurationMs: Long,
        suspendedDurationMs: Long,
        suspensionCount: Int,
        threadMigrationCount: Int,
        generation: Long,
        outcome: CoroutineExecutionOutcome,
    )
}

internal fun interface CoroutineSegmentSink {
    fun segment(
        token: Long,
        generation: Long,
        owner: String,
        startedMs: Long,
        endedMs: Long,
        entryJavaThreadId: Long,
        exitJavaThreadId: Long,
        entryTid: Long,
        exitTid: Long,
        suspended: Boolean,
        outcome: CoroutineExecutionOutcome,
    )
}

/** A point-in-time observation; it does not end the segment or release its thread. */
internal data class RunningCoroutineSegment(
    val token: Long,
    val generation: Long,
    val owner: String,
    val startedMs: Long,
    val observedMs: Long,
    val entryJavaThreadId: Long,
    val entryTid: Long,
)

/**
 * Bounded weak-identity state for Kotlin coroutine state machines.
 *
 * A state machine normally executes one segment at a time. Primitive parallel arrays keep resume
 * free of state/event allocations; the only per-coroutine registry allocation is a weak reference.
 */
internal class CoroutineExecutionTracker(
    capacity: Int = DEFAULT_CAPACITY,
    shardCount: Int = DEFAULT_SHARD_COUNT,
    private val clock: RuntimeLongSource,
    private val threadId: RuntimeLongSource,
    private val nativeThreadId: RuntimeLongSource = threadId,
    private val onComplete: CoroutineExecutionSink,
    private val onSegment: CoroutineSegmentSink = CoroutineSegmentSink { _, _, _, _, _, _, _, _, _, _, _ -> },
    private val onEviction: () -> Unit = {},
    private val onInvalidTransition: () -> Unit = {},
    private val onResolutionMiss: () -> Unit = {},
    private val causal: RuntimeCausalCoroutineTelemetry? = null,
) {
    private val nextToken = AtomicLong()
    private val shards: Array<Shard>

    init {
        require(capacity > 0)
        require(shardCount > 0 && shardCount <= capacity)
        require(capacity % shardCount == 0)
        shards = Array(shardCount) { Shard(capacity / shardCount, causal != null) }
    }

    fun enter(
        continuation: Any?, owner: String, collectNew: Boolean, generation: Long = 0L,
        siteId: Long = 0L, source: String = owner, line: Int = 0, origin: Int = 0,
    ): Long {
        if (continuation == null) return NO_TOKEN
        val now = clock.getAsLong()
        val currentThreadId = threadId.getAsLong()
        val currentTid = nativeThreadId.getAsLong()
        val shard = shardFor(continuation)
        var invalidTransition = false
        var evicted = false
        var created = false
        var capacityRejected = false
        var causalBinding: RuntimeCausalCoroutineLaunch? = null
        val token = tryDiagnosticLock(shard.lock, NO_TOKEN) {
            var reusableIndex = NO_INDEX
            for (index in shard.references.indices) {
                val existing = shard.references[index]?.get()
                if (existing === continuation) {
                    if (shard.phases[index] != PHASE_SUSPENDED) {
                        invalidTransition = true
                        return@tryDiagnosticLock NO_TOKEN
                    }
                    shard.suspendedDurationMs[index] = saturatingAdd(
                        shard.suspendedDurationMs[index],
                        nonNegativeDelta(now, shard.segmentStartMs[index]),
                    )
                    val previousJavaId = shard.threadIds[index]
                    val previousTid = shard.nativeThreadIds[index]
                    val migrated = if (previousJavaId > 0L && currentThreadId > 0L) {
                        previousJavaId != currentThreadId
                    } else {
                        previousTid > 0L && currentTid > 0L && previousTid != currentTid
                    }
                    if (migrated) {
                        shard.threadMigrationCounts[index] = saturatingIncrement(shard.threadMigrationCounts[index])
                    }
                    shard.threadIds[index] = currentThreadId
                    shard.nativeThreadIds[index] = currentTid
                    shard.segmentStartMs[index] = now
                    shard.phases[index] = PHASE_RUNNING
                    causalBinding = shard.causalBindings?.get(index)
                    return@tryDiagnosticLock shard.tokens[index]
                }
                if (reusableIndex == NO_INDEX && existing == null) reusableIndex = index
            }
            if (!collectNew) return@tryDiagnosticLock NO_TOKEN
            if (reusableIndex == NO_INDEX && causal?.isEnabled() == true) {
                capacityRejected = true
                return@tryDiagnosticLock NO_TOKEN
            }
            val target = if (reusableIndex != NO_INDEX) {
                reusableIndex
            } else {
                val selected = shard.evictionCursor
                shard.evictionCursor = (selected + 1) % shard.references.size
                evicted = true
                selected
            }
            val newToken = nextNonZeroToken()
            created = true
            shard.references[target] = WeakReference(continuation)
            shard.owners[target] = owner
            shard.tokens[target] = newToken
            shard.segmentStartMs[target] = now
            shard.activeDurationMs[target] = 0L
            shard.suspendedDurationMs[target] = 0L
            shard.threadIds[target] = currentThreadId
            shard.nativeThreadIds[target] = currentTid
            shard.suspensionCounts[target] = 0
            shard.threadMigrationCounts[target] = 0
            shard.generations[target] = generation
            shard.phases[target] = PHASE_RUNNING
            shard.causalBindings?.set(target, null)
            newToken
        }
        if (invalidTransition) onInvalidTransition()
        if (evicted) onEviction()
        if (capacityRejected) causal?.capacityRejected()
        if (token != NO_TOKEN && created && causal != null) {
            causalBinding = causal.bindFirstSegment(continuation, siteId, source, line, origin)
            tryDiagnosticLock(shard.lock, Unit) {
                for (index in shard.references.indices) {
                    if (shard.tokens[index] == token && shard.references[index]?.get() === continuation) {
                        shard.causalBindings?.set(index, causalBinding)
                        break
                    }
                }
            }
        }
        causal?.enter(causalBinding, token, generation)
        return token
    }

    fun exit(
        token: Long,
        continuation: Any?,
        suspended: Boolean,
        outcome: CoroutineExecutionOutcome,
    ) {
        if (token == NO_TOKEN || continuation == null) return
        val now = clock.getAsLong()
        val exitThreadId = threadId.getAsLong()
        val exitTid = nativeThreadId.getAsLong()
        val shard = shardFor(continuation)
        var invalidTransition = false
        var resolutionMiss = false
        var completed = false
        var completedOwner = ""
        var completedActiveDurationMs = 0L
        var completedSuspendedDurationMs = 0L
        var completedSuspensionCount = 0
        var completedThreadMigrationCount = 0
        var completedGeneration = 0L
        var segmentObserved = false
        var segmentOwner = ""
        var segmentStartMs = 0L
        var segmentEntryThreadId = 0L
        var segmentEntryTid = 0L
        var segmentGeneration = 0L
        tryDiagnosticLock(shard.lock, Unit) {
            var target = NO_INDEX
            for (index in shard.references.indices) {
                if (shard.references[index]?.get() === continuation && shard.tokens[index] == token) {
                    target = index
                    break
                }
            }
            if (target == NO_INDEX) {
                resolutionMiss = true
                return@tryDiagnosticLock
            }
            if (shard.phases[target] != PHASE_RUNNING) {
                invalidTransition = true
                return@tryDiagnosticLock
            }
            segmentObserved = true
            segmentOwner = checkNotNull(shard.owners[target])
            segmentStartMs = shard.segmentStartMs[target]
            segmentEntryThreadId = shard.threadIds[target]
            segmentEntryTid = shard.nativeThreadIds[target]
            segmentGeneration = shard.generations[target]
            shard.activeDurationMs[target] = saturatingAdd(
                shard.activeDurationMs[target],
                nonNegativeDelta(now, shard.segmentStartMs[target]),
            )
            if (suspended) {
                shard.suspensionCounts[target] = saturatingIncrement(shard.suspensionCounts[target])
                shard.segmentStartMs[target] = now
                shard.phases[target] = PHASE_SUSPENDED
                return@tryDiagnosticLock
            }
            completed = true
            completedOwner = checkNotNull(shard.owners[target])
            completedActiveDurationMs = shard.activeDurationMs[target]
            completedSuspendedDurationMs = shard.suspendedDurationMs[target]
            completedSuspensionCount = shard.suspensionCounts[target]
            completedThreadMigrationCount = shard.threadMigrationCounts[target]
            completedGeneration = shard.generations[target]
            shard.clear(target)
        }
        if (resolutionMiss) onResolutionMiss()
        if (invalidTransition) onInvalidTransition()
        if (segmentObserved) {
            onSegment.segment(token, segmentGeneration, segmentOwner, segmentStartMs, now,
                segmentEntryThreadId, exitThreadId, segmentEntryTid, exitTid, suspended, outcome)
        }
        if (completed) {
            onComplete.complete(
                completedOwner,
                completedActiveDurationMs,
                completedSuspendedDurationMs,
                completedSuspensionCount,
                completedThreadMigrationCount,
                completedGeneration,
                outcome,
            )
        }
    }

    fun clear() {
        shards.forEach { shard ->
            shard.lock.withLock {
                for (index in shard.references.indices) shard.clear(index)
                shard.evictionCursor = 0
            }
        }
    }

    /** Copy under shard locks, then publish without holding a lock across writer admission. */
    fun snapshotRunning(report: (RunningCoroutineSegment) -> Unit) {
        val running = ArrayList<RunningCoroutineSegment>()
        shards.forEach { shard ->
            shard.lock.withLock {
                for (index in shard.references.indices) {
                    if (shard.phases[index] != PHASE_RUNNING || shard.references[index]?.get() == null) continue
                    val observedMs = clock.getAsLong()
                    val startedMs = shard.segmentStartMs[index]
                    if (observedMs < startedMs) continue
                    running.add(RunningCoroutineSegment(
                        shard.tokens[index], shard.generations[index], checkNotNull(shard.owners[index]),
                        startedMs, observedMs, shard.threadIds[index], shard.nativeThreadIds[index],
                    ))
                }
            }
        }
        running.forEach(report)
    }

    internal fun retainedEntryCount(): Int {
        var result = 0
        shards.forEach { shard ->
            shard.lock.withLock {
                for (reference in shard.references) {
                    if (reference?.get() != null) result++
                }
            }
        }
        return result
    }

    private fun shardFor(continuation: Any): Shard {
        val hash = System.identityHashCode(continuation) and Int.MAX_VALUE
        return shards[hash % shards.size]
    }

    private fun nextNonZeroToken(): Long {
        var token = nextToken.incrementAndGet()
        if (token == NO_TOKEN) token = nextToken.incrementAndGet()
        return token
    }

    private class Shard(capacity: Int, causalEnabled: Boolean) {
        val lock = ReentrantLock()
        val references = arrayOfNulls<WeakReference<Any>>(capacity)
        val owners = arrayOfNulls<String>(capacity)
        val tokens = LongArray(capacity)
        val segmentStartMs = LongArray(capacity)
        val activeDurationMs = LongArray(capacity)
        val suspendedDurationMs = LongArray(capacity)
        val threadIds = LongArray(capacity)
        val nativeThreadIds = LongArray(capacity)
        val suspensionCounts = IntArray(capacity)
        val threadMigrationCounts = IntArray(capacity)
        val generations = LongArray(capacity)
        val phases = ByteArray(capacity)
        val causalBindings = if (causalEnabled) arrayOfNulls<RuntimeCausalCoroutineLaunch>(capacity) else null
        var evictionCursor = 0

        fun clear(index: Int) {
            references[index] = null
            owners[index] = null
            tokens[index] = NO_TOKEN
            segmentStartMs[index] = 0L
            activeDurationMs[index] = 0L
            suspendedDurationMs[index] = 0L
            threadIds[index] = 0L
            nativeThreadIds[index] = 0L
            suspensionCounts[index] = 0
            threadMigrationCounts[index] = 0
            generations[index] = 0L
            phases[index] = PHASE_EMPTY
            causalBindings?.set(index, null)
        }
    }

    private companion object {
        const val DEFAULT_CAPACITY = 1_024
        const val DEFAULT_SHARD_COUNT = 16
        const val NO_TOKEN = 0L
        const val NO_INDEX = -1
        const val PHASE_EMPTY: Byte = 0
        const val PHASE_RUNNING: Byte = 1
        const val PHASE_SUSPENDED: Byte = 2

        fun nonNegativeDelta(end: Long, start: Long): Long = (end - start).coerceAtLeast(0L)

        fun saturatingIncrement(value: Int): Int = if (value == Int.MAX_VALUE) value else value + 1
    }
}
