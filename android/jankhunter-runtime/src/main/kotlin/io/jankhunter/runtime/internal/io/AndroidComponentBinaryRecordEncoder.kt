package io.jankhunter.runtime.internal.io

/** Validates and encodes privacy-bounded Android component and Binder metadata. */
internal class AndroidComponentBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    fun processState(
        uiVisibility: Long,
        processImportance: Long,
        androidImportance: Long,
        reason: Long,
    ) {
        require(uiVisibility in Jhlog.PROCESS_UI_UNKNOWN..Jhlog.PROCESS_UI_VISIBLE)
        require(processImportance in Jhlog.PROCESS_IMPORTANCE_UNKNOWN..Jhlog.PROCESS_IMPORTANCE_CACHED)
        require(androidImportance in 0L..MAX_UINT32)
        require(reason in Jhlog.PROCESS_STATE_PERIODIC_SAMPLE..Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE)
        val payload = sink.payload()
            .uvarint(uiVisibility)
            .uvarint(processImportance)
            .uvarint(androidImportance)
            .uvarint(reason)
        sink.emit(Jhlog.TYPE_PROCESS_STATE, 0L, payload, sink.producerContext())
    }

    fun androidComponent(
        componentId: Long,
        componentName: String?,
        action: String?,
        instanceId: Long,
        flowId: Long,
        kind: Long,
        stage: Long,
        outcome: Long,
        durationUs: Long,
        componentFlags: Long,
    ) {
        require(componentId != 0L)
        require(instanceId > 0L)
        require(flowId > 0L)
        require(validComponentStage(kind, stage))
        require(outcome in Jhlog.COMPONENT_OUTCOME_UNKNOWN..Jhlog.COMPONENT_OUTCOME_CANCELLED)
        require(durationUs >= 0L)
        require(componentFlags and Jhlog.COMPONENT_FLAG_KNOWN_MASK.inv() == 0L)
        sink.defineStableSymbol(componentId, componentName)
        val payload = sink.payload()
            .stableSymbolRef(componentId)
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, action))
            .uvarint(instanceId)
            .uvarint(flowId)
            .uvarint(kind)
            .uvarint(stage)
            .uvarint(outcome)
            .uvarint(durationUs)
            .uvarint(componentFlags)
        sink.emit(Jhlog.TYPE_ANDROID_COMPONENT, 0L, payload, sink.producerContext())
    }

    fun binderTransaction(
        descriptor: String?,
        method: String?,
        callId: Long,
        direction: Long,
        transactionCode: Long,
        outcome: Long,
        failureKind: Long,
        durationUs: Long,
        binderFlags: Long,
        mainThread: Boolean,
    ) {
        require(callId > 0L)
        require(direction in Jhlog.BINDER_DIRECTION_CLIENT..Jhlog.BINDER_DIRECTION_SERVER)
        require(transactionCode in 0L..MAX_UINT32)
        require(outcome in Jhlog.BINDER_OUTCOME_SUCCESS..Jhlog.BINDER_OUTCOME_UNHANDLED)
        require(failureKind in Jhlog.BINDER_FAILURE_NONE..Jhlog.BINDER_FAILURE_OTHER)
        require(outcome != Jhlog.BINDER_OUTCOME_FAILURE || failureKind != Jhlog.BINDER_FAILURE_NONE)
        require(outcome == Jhlog.BINDER_OUTCOME_FAILURE || failureKind == Jhlog.BINDER_FAILURE_NONE)
        require(durationUs >= 0L)
        require(binderFlags and Jhlog.BINDER_FLAG_KNOWN_MASK.inv() == 0L)
        val payload = sink.payload()
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, descriptor))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, method))
            .uvarint(callId)
            .uvarint(direction)
            .uvarint(transactionCode)
            .uvarint(outcome)
            .uvarint(failureKind)
            .uvarint(durationUs)
            .uvarint(binderFlags)
        sink.emit(
            Jhlog.TYPE_BINDER_TRANSACTION,
            if (mainThread) BinaryLogWriter.FLAG_THREAD_MAIN else 0L,
            payload,
            sink.producerContext(),
        )
    }

    private fun validComponentStage(kind: Long, stage: Long): Boolean = when (kind) {
        Jhlog.COMPONENT_KIND_SERVICE -> stage in Jhlog.COMPONENT_SERVICE_CREATED..Jhlog.COMPONENT_SERVICE_TIMEOUT
        Jhlog.COMPONENT_KIND_RECEIVER -> stage in Jhlog.COMPONENT_RECEIVER_STARTED..Jhlog.COMPONENT_RECEIVER_FINISHED
        else -> false
    }

    private companion object {
        const val MAX_UINT32 = 0xffff_ffffL
    }
}
