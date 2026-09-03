package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue

/** Writer-owned bounded reuse for the highest-volume asynchronous event type. */
internal class PendingDatabaseEventPool(capacity: Int) {
    private val available = BoundedMpscQueue<PendingDatabaseEvent>(capacity)

    fun acquire(
        producerContext: LogEventContext?,
        sourceId: Long,
        sourceName: String?,
        query: String?,
        framework: Long,
        operation: Long,
        outcome: Long,
        durationUs: Long,
        mainThread: Boolean,
        failureKind: Long,
        boundary: Long,
        statementFingerprint: Long,
        resultKnown: Boolean,
        resultKind: Long,
        resultCountBucket: Long,
        transactionId: Long,
        statementToken: Long,
        phaseMask: Long,
        poolWaitUs: Long,
        lockWaitUs: Long,
        executeUs: Long,
        materializeUs: Long,
    ): PendingDatabaseEvent {
        val event = available.poll() ?: PendingDatabaseEvent(this)
        return event.initialize(
            producerContext,
            sourceId,
            sourceName,
            query,
            framework,
            operation,
            outcome,
            durationUs,
            mainThread,
            failureKind,
            boundary,
            statementFingerprint,
            resultKnown,
            resultKind,
            resultCountBucket,
            transactionId,
            statementToken,
            phaseMask,
            poolWaitUs,
            lockWaitUs,
            executeUs,
            materializeUs,
        )
    }

    fun release(event: PendingDatabaseEvent) {
        event.clearForRecycle()
        available.tryOffer(event)
    }

}
