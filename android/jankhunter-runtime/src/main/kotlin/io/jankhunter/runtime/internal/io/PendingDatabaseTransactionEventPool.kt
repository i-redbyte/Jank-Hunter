package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.concurrent.BoundedMpscQueue

/** Writer-owned bounded reuse for database transaction lifecycle entries. */
internal class PendingDatabaseTransactionEventPool(capacity: Int) {
    private val available = BoundedMpscQueue<PendingDatabaseTransactionEvent>(capacity)

    fun acquire(
        producerContext: LogEventContext?,
        sourceId: Long,
        sourceName: String?,
        transactionId: Long,
        parentId: Long,
        stage: Long,
        mode: Long,
        outcome: Long,
        failureKind: Long,
        durationUs: Long,
        statementCount: Long,
        readCount: Long,
        writeCount: Long,
        mainThread: Boolean,
    ): PendingDatabaseTransactionEvent {
        val event = available.poll() ?: PendingDatabaseTransactionEvent(this)
        return event.initialize(
            producerContext,
            sourceId,
            sourceName,
            transactionId,
            parentId,
            stage,
            mode,
            outcome,
            failureKind,
            durationUs,
            statementCount,
            readCount,
            writeCount,
            mainThread,
        )
    }

    fun release(event: PendingDatabaseTransactionEvent) {
        event.clearForRecycle()
        available.tryOffer(event)
    }
}
