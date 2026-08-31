package io.jankhunter.runtime

import java.lang.ref.WeakReference

/** Operation semantics for dependency-free custom database/ORM tracing. */
enum class JankHunterDatabaseOperation(internal val wireValue: Int) {
    QUERY(1),
    INSERT(2),
    UPDATE(3),
    DELETE(4),
    EXECUTE(5),
    STATEMENT(6),
}

/** Measured boundary; custom adapters must report only the phase they actually surround. */
enum class JankHunterDatabaseBoundary(internal val wireValue: Int) {
    DISPATCH(1),
    EXECUTE(2),
    MATERIALIZE(3),
    MANUAL(4),
}

/**
 * A phase measured by an integration adapter. Phases must not overlap and their sum must fit
 * inside the enclosing database call. The runtime never infers a phase from total duration.
 */
enum class JankHunterDatabasePhase(internal val wireMask: Long) {
    POOL_WAIT(1L shl 0),
    LOCK_WAIT(1L shl 1),
    EXECUTE(1L shl 2),
    MATERIALIZE(1L shl 3),
}

/** Result count semantics. The runtime stores only a coarse bucket, never the exact value. */
enum class JankHunterDatabaseResultKind(internal val wireValue: Int) {
    ROWS(1),
    AFFECTED_ROWS(2),
}

enum class JankHunterDatabaseTransactionMode(internal val wireValue: Int) {
    DEFERRED(1),
    IMMEDIATE(2),
    EXCLUSIVE(3),
    READ_ONLY(4),
}

enum class JankHunterDatabaseTransactionOutcome {
    SUCCESS,
    ROLLBACK,
    FAILURE,
}

class JankHunterDatabaseCallToken internal constructor(
    internal val sourceId: Long,
    internal val sourceName: String,
    internal val query: String?,
    internal val fingerprint: Long,
    internal val operation: Int,
    internal val boundary: Int,
    internal val startedNanos: Long,
) {
    private var completed = false
    private var phaseMask = 0L
    private var poolWaitUs = 0L
    private var lockWaitUs = 0L
    private var executeUs = 0L
    private var materializeUs = 0L

    @Synchronized
    internal fun completeOnce(): Boolean {
        if (completed) return false
        completed = true
        return true
    }

    @Synchronized
    internal fun recordPhase(phase: JankHunterDatabasePhase, durationNanos: Long): Boolean {
        if (completed || durationNanos < 0L) return false
        val durationUs = durationNanos / NANOS_PER_MICROSECOND
        phaseMask = phaseMask or phase.wireMask
        when (phase) {
            JankHunterDatabasePhase.POOL_WAIT -> poolWaitUs = saturatingAdd(poolWaitUs, durationUs)
            JankHunterDatabasePhase.LOCK_WAIT -> lockWaitUs = saturatingAdd(lockWaitUs, durationUs)
            JankHunterDatabasePhase.EXECUTE -> executeUs = saturatingAdd(executeUs, durationUs)
            JankHunterDatabasePhase.MATERIALIZE -> materializeUs = saturatingAdd(materializeUs, durationUs)
        }
        return true
    }

    @Synchronized
    internal fun validatedPhaseMask(totalDurationUs: Long): Long {
        if (phaseMask == 0L || poolWaitUs > totalDurationUs) return 0L
        var remaining = totalDurationUs - poolWaitUs
        if (lockWaitUs > remaining) return 0L
        remaining -= lockWaitUs
        if (executeUs > remaining) return 0L
        remaining -= executeUs
        if (materializeUs > remaining) return 0L
        return phaseMask
    }

    @Synchronized
    internal fun phaseDurationUs(phase: JankHunterDatabasePhase): Long = when (phase) {
        JankHunterDatabasePhase.POOL_WAIT -> poolWaitUs
        JankHunterDatabasePhase.LOCK_WAIT -> lockWaitUs
        JankHunterDatabasePhase.EXECUTE -> executeUs
        JankHunterDatabasePhase.MATERIALIZE -> materializeUs
    }

    private fun saturatingAdd(left: Long, right: Long): Long {
        return if (right > Long.MAX_VALUE - left) Long.MAX_VALUE else left + right
    }

    private companion object {
        const val NANOS_PER_MICROSECOND = 1_000L
    }
}

class JankHunterDatabaseTransactionToken internal constructor(
    internal val id: Long,
    internal val sourceId: Long,
    internal val sourceName: String,
    internal val mode: Long,
    internal val parentId: Long,
    internal val startedNanos: Long,
    internal val parent: WeakReference<JankHunterDatabaseTransactionToken>?,
) {
    @Volatile
    private var completed = false
    internal var statementCount = 0L
        private set
    internal var readCount = 0L
        private set
    internal var writeCount = 0L
        private set

    @Synchronized
    internal fun completeOnce(): Boolean {
        if (completed) return false
        completed = true
        return true
    }

    internal fun isCompleted(): Boolean = completed

    @Synchronized
    internal fun recordStatement(operation: Long) {
        if (completed) return
        statementCount++
        if (operation == JankHunterDatabaseOperation.QUERY.wireValue.toLong()) {
            readCount++
        } else {
            writeCount++
        }
    }
}
