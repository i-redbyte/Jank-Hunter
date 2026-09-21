package io.jankhunter.runtime.internal.io

/** Reusable context carrier owned by the thread-confined writer; no allocation per event. */
internal class BinaryRecordContext {
    var screenId = 0L
    var ownerId = 0L
    var stableOwnerAlias = 0L
    var hasStableOwner = false
    var operationId = 0L

    fun set(
        screenId: Long,
        ownerId: Long,
        stableOwnerAlias: Long = 0L,
        hasStableOwner: Boolean = false,
        operationId: Long = 0L,
    ): BinaryRecordContext {
        this.screenId = screenId
        this.ownerId = ownerId
        this.stableOwnerAlias = stableOwnerAlias
        this.hasStableOwner = hasStableOwner
        this.operationId = operationId
        return this
    }

    fun clear(): BinaryRecordContext = set(0L, 0L)
}

/** Encodes record envelopes and owns the cross-record delta/context state. */
internal class BinaryRecordEncoder(
    private val segmentStartElapsedUs: Long,
) {
    private val recordBody = BinaryPayload(256)
    private val encodedRecord = BinaryPayload(320)
    private var lastTimedRecordUs = segmentStartElapsedUs
    private var hasLastContext = false
    private var lastScreenId = 0L
    private var lastOwnerId = 0L
    private var lastStableOwnerAlias = 0L
    private var lastHasStableOwner = false
    private var lastOperationId = 0L

    fun encode(
        recordType: Int,
        attributes: Long,
        payload: BinaryPayload,
        context: BinaryRecordContext?,
        producer: ProducerMetadataBuffer?,
    ): BinaryPayload {
        var envelopeFlags = 0L
        if (producer != null) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_HAS_TIME or Jhlog.ENVELOPE_HAS_THREAD
        if (context != null) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_HAS_CONTEXT
        val sameContext = context != null && sameAsLast(context)
        if (sameContext) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_SAME_CONTEXT
        if (attributes != 0L) envelopeFlags = envelopeFlags or Jhlog.ENVELOPE_HAS_ATTRIBUTES

        val body = recordBody.clear()
            .uvarint(recordType.toLong())
            .uvarint(envelopeFlags)
        if (producer != null) {
            body.svarint(producer.elapsedUs - lastTimedRecordUs)
            body.uvarint(producer.threadId)
        }
        if (context != null && !sameContext) {
            var presence = 0L
            if (context.screenId != 0L) presence = presence or Jhlog.CONTEXT_SCREEN
            if (context.ownerId != 0L || context.hasStableOwner) presence = presence or Jhlog.CONTEXT_OWNER
            if (context.operationId != 0L) presence = presence or Jhlog.CONTEXT_OPERATION
            body.uvarint(presence)
            if (presence and Jhlog.CONTEXT_SCREEN != 0L) body.symbolRef(context.screenId)
            if (presence and Jhlog.CONTEXT_OWNER != 0L) {
                if (context.hasStableOwner) {
                    body.stableSymbolAlias(context.stableOwnerAlias)
                } else {
                    body.symbolRef(context.ownerId)
                }
            }
            if (presence and Jhlog.CONTEXT_OPERATION != 0L) body.uvarint(context.operationId)
        }
        if (attributes != 0L) body.uvarint(attributes)
        body.bytes(payload)
        return encodedRecord.clear().uvarint(body.size.toLong()).bytes(body)
    }

    fun commit(producer: ProducerMetadataBuffer?, context: BinaryRecordContext?) {
        if (producer != null) lastTimedRecordUs = producer.elapsedUs
        if (context == null) return
        hasLastContext = true
        lastScreenId = context.screenId
        lastOwnerId = context.ownerId
        lastStableOwnerAlias = context.stableOwnerAlias
        lastHasStableOwner = context.hasStableOwner
        lastOperationId = context.operationId
    }

    fun reset() {
        lastTimedRecordUs = segmentStartElapsedUs
        hasLastContext = false
    }

    private fun sameAsLast(context: BinaryRecordContext): Boolean {
        return hasLastContext &&
            lastScreenId == context.screenId &&
            lastOwnerId == context.ownerId &&
            lastStableOwnerAlias == context.stableOwnerAlias &&
            lastHasStableOwner == context.hasStableOwner &&
            lastOperationId == context.operationId
    }
}
