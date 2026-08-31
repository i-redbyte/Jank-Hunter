package io.jankhunter.runtime

import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.Jhlog

internal class RuntimeIOTelemetry(
    private val access: RuntimeTelemetryAccess,
) {
    fun isEnabled(): Boolean = access.config?.ioTracingEnabled() == true

    fun record(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        ownerName: String?,
        outcome: JankHunterIOOutcome,
    ) {
        if (!isEnabled()) return
        RuntimeHookGuard.run {
            recordInternal(operation, durationNanos, bytes, ownerName, outcome, 0L, null)
        }
    }

    fun <T> trace(
        operation: JankHunterIOOperation,
        bytes: Long,
        ownerName: String?,
        block: () -> T,
    ): T {
        if (!isEnabled()) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        var outcome = JankHunterIOOutcome.FAILURE
        try {
            return block().also { outcome = JankHunterIOOutcome.SUCCESS }
        } finally {
            record(operation, SystemClock.elapsedRealtimeNanos() - startedAt, bytes, ownerName, outcome)
        }
    }

    fun recordAutomatic(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        outcome: JankHunterIOOutcome,
        sourceId: Long,
        sourceName: String,
    ) {
        if (!isEnabled()) return
        RuntimeHookGuard.run {
            recordInternal(operation, durationNanos, bytes, null, outcome, sourceId, sourceName)
        }
    }

    fun recordInternal(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        ownerName: String?,
        outcome: JankHunterIOOutcome,
        sourceId: Long,
        sourceName: String?,
    ) {
        access.ensureContextRecorded(ownerOverride = firstContextValue(ownerName, access.currentOwnerOrNull()))
        val mainLooper = Looper.getMainLooper()
        val bytesKnown = bytes >= 0L
        access.writer?.io(
            operation = operation.wireValue,
            durationUs = durationNanos.coerceAtLeast(0L) / NANOS_PER_MICROSECOND,
            bytes = if (bytesKnown) bytes else 0L,
            mainThread = mainLooper != null && Looper.myLooper() === mainLooper,
            sourceId = sourceId,
            sourceName = sourceName,
            outcome = when (outcome) {
                JankHunterIOOutcome.SUCCESS -> Jhlog.IO_OUTCOME_SUCCESS
                JankHunterIOOutcome.FAILURE -> Jhlog.IO_OUTCOME_FAILURE
            },
            bytesKnown = bytesKnown,
        )
    }

    companion object {
        const val UNKNOWN_BYTES = -1L
        private const val NANOS_PER_MICROSECOND = 1_000L
    }
}
