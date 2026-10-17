package io.jankhunter.runtime

import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.internal.io.Jhlog

internal class RuntimeIOTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val concurrency: RuntimeConcurrencyTelemetry? = null,
) {
    fun captureCausalContext(): JankHunterCausalContext? = concurrency?.captureCausalContext()

    fun isEnabled(): Boolean = access.isFeatureActive(JankHunterRuntimeFeature.BYTECODE_IO)

    fun record(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        ownerName: String?,
        outcome: JankHunterIOOutcome,
    ) {
        if (!access.isFeatureActive(JankHunterRuntimeFeature.RUNTIME_IO)) return
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
        if (!access.isFeatureActive(JankHunterRuntimeFeature.RUNTIME_IO)) return block()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        val causalContext = captureCausalContext()
        var outcome = JankHunterIOOutcome.FAILURE
        try {
            return block().also { outcome = JankHunterIOOutcome.SUCCESS }
        } finally {
            RuntimeHookGuard.run {
                recordInternal(operation, SystemClock.elapsedRealtimeNanos() - startedAt, bytes, ownerName,
                    outcome, 0L, null, causalContext)
            }
        }
    }

    fun recordAutomatic(
        operation: JankHunterIOOperation,
        durationNanos: Long,
        bytes: Long,
        outcome: JankHunterIOOutcome,
        sourceId: Long,
        sourceName: String,
        causalContext: JankHunterCausalContext? = null,
    ) {
        if (!isEnabled()) return
        RuntimeHookGuard.run {
            recordInternal(operation, durationNanos, bytes, null, outcome, sourceId, sourceName, causalContext)
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
        causalContext: JankHunterCausalContext? = null,
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
        concurrency?.recordCost(
            category = when (operation) {
                JankHunterIOOperation.FILE_READ -> RuntimeCausalOperationKind.FILE_READ
                JankHunterIOOperation.FILE_WRITE -> RuntimeCausalOperationKind.FILE_WRITE
                JankHunterIOOperation.FILE_SYNC -> RuntimeCausalOperationKind.FILE_SYNC
                JankHunterIOOperation.CONTENT_READ -> RuntimeCausalOperationKind.CONTENT_READ
                JankHunterIOOperation.CONTENT_WRITE -> RuntimeCausalOperationKind.CONTENT_WRITE
            },
            durationNanos = durationNanos.coerceAtLeast(-1L),
            readBytes = if ((operation == JankHunterIOOperation.FILE_READ ||
                operation == JankHunterIOOperation.CONTENT_READ) && bytesKnown &&
                outcome == JankHunterIOOutcome.SUCCESS) bytes else -1L,
            writeBytes = if ((operation == JankHunterIOOperation.FILE_WRITE ||
                operation == JankHunterIOOperation.CONTENT_WRITE) && bytesKnown &&
                outcome == JankHunterIOOutcome.SUCCESS) bytes else -1L,
            sourceSiteId = 0L,
            // Legacy sourceName can be a path or arbitrary label, not a verified code location.
            sourceMethod = null,
            sourceLine = 0,
            sourceOrigin = 0,
            parent = causalContext,
            executionTid = android.os.Process.myTid().toLong(),
        )
    }

    companion object {
        const val UNKNOWN_BYTES = -1L
        private const val NANOS_PER_MICROSECOND = 1_000L
    }
}
