package io.jankhunter.runtime.internal.io

/** Encodes one bounded runtime-call block while owning its segment-local edge registry and scratch columns. */
internal class RuntimeCallBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    private val edges = RuntimeEdgeRegistry()
    private val screenIds = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
    private val callerAliases = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
    private val calleeAliases = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
    private val numericValues = LongArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
    private val edgeDefinitions = ByteArray((Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS + 7) / 8)
    private val edgeTupleRows = IntArray(Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS)
    private val numericCodec = RuntimeNumericColumnCodec()

    fun runtimeCalls(batch: RuntimeCallBatch) {
        if (batch.size == 0) return
        require(batch.size <= Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS) {
            "Runtime call block has ${batch.size} rows; maximum is ${Jhlog.MAX_RUNTIME_CALL_BLOCK_ROWS}"
        }
        resolveSymbols(batch)
        val payload = sink.payload().uvarint((batch.size.toLong() shl 1) or COLUMNAR_EDGE_TUPLES)
        encodeEdges(batch, payload)
        encodeNumericColumn(batch, payload, COLUMN_COUNT, sparse = true, defaultValue = 1L)
        encodeNumericColumn(batch, payload, COLUMN_TOTAL, sparse = true)
        encodeNumericColumn(batch, payload, COLUMN_MAX, sparse = true)
        sink.emitSemantic(
            recordType = Jhlog.TYPE_RUNTIME_CALL,
            attributes = 0L,
            payload = payload,
            context = null,
            semanticEventCount = batch.size.toLong(),
        )
    }

    private fun resolveSymbols(batch: RuntimeCallBatch) {
        for (index in 0 until batch.size) {
            screenIds[index] = sink.optionalSymbolId(BinaryLogWriter.DICT_SCREEN, batch.screen(index))
            callerAliases[index] = sink.defineStableSymbol(batch.callerId(index), batch.callerName(index))
            calleeAliases[index] = sink.defineStableSymbol(batch.calleeId(index), batch.calleeName(index))
        }
    }

    private fun encodeEdges(batch: RuntimeCallBatch, payload: BinaryPayload) {
        val definitionBytes = (batch.size + 7) / 8
        edgeDefinitions.fill(0, 0, definitionBytes)
        var tupleCount = 0
        for (index in 0 until batch.size) {
            val token = edges.resolve(
                screen = screenIds[index],
                caller = callerAliases[index],
                operation = batch.operationId(index),
                callee = calleeAliases[index],
            )
            numericValues[index] = token ushr 1
            if (token and EDGE_DEFINITION != 0L) {
                edgeDefinitions[index ushr 3] =
                    (edgeDefinitions[index ushr 3].toInt() or (1 shl (index and 7))).toByte()
            }
            if (numericValues[index] == EDGE_INLINE || token and EDGE_DEFINITION != 0L) {
                edgeTupleRows[tupleCount++] = index
            }
        }
        payload.bytes(edgeDefinitions, definitionBytes)
        numericCodec.encode(numericValues, batch.size, payload)
        encodeTupleColumns(batch, tupleCount, payload)
    }

    private fun encodeTupleColumns(batch: RuntimeCallBatch, tupleCount: Int, payload: BinaryPayload) {
        if (tupleCount == 0) return
        for (tuple in 0 until tupleCount) numericValues[tuple] = screenIds[edgeTupleRows[tuple]]
        numericCodec.encode(numericValues, tupleCount, payload, sparse = true)
        for (tuple in 0 until tupleCount) numericValues[tuple] = callerAliases[edgeTupleRows[tuple]]
        numericCodec.encode(numericValues, tupleCount, payload)
        for (tuple in 0 until tupleCount) {
            numericValues[tuple] = BinaryLogWriter.nonNegative(batch.operationId(edgeTupleRows[tuple]))
        }
        numericCodec.encode(numericValues, tupleCount, payload, sparse = true)
        for (tuple in 0 until tupleCount) numericValues[tuple] = calleeAliases[edgeTupleRows[tuple]]
        numericCodec.encode(numericValues, tupleCount, payload)
    }

    private fun encodeNumericColumn(
        batch: RuntimeCallBatch,
        payload: BinaryPayload,
        column: Int,
        sparse: Boolean,
        defaultValue: Long = 0L,
    ) {
        when (column) {
            COLUMN_COUNT -> for (index in 0 until batch.size) {
                numericValues[index] = BinaryLogWriter.nonNegative(batch.count(index))
            }
            COLUMN_TOTAL -> for (index in 0 until batch.size) {
                numericValues[index] = BinaryLogWriter.nonNegative(batch.totalMs(index))
            }
            COLUMN_MAX -> for (index in 0 until batch.size) {
                numericValues[index] = BinaryLogWriter.nonNegative(batch.maxMs(index))
            }
            else -> error("Unsupported runtime numeric column $column")
        }
        numericCodec.encode(numericValues, batch.size, payload, sparse, defaultValue)
    }

    private companion object {
        const val EDGE_INLINE = 0L
        const val EDGE_DEFINITION = 1L
        const val COLUMNAR_EDGE_TUPLES = 1L
        const val COLUMN_COUNT = 0
        const val COLUMN_TOTAL = 1
        const val COLUMN_MAX = 2
    }
}
