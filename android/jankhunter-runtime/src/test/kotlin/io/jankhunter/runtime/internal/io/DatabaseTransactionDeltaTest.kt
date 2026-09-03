package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class DatabaseTransactionDeltaTest {
    @Test
    fun sharesTransactionDeltaStateAcrossDatabaseRecordTypes() {
        val sink = CapturingSink()
        val encoder = DatabaseBinaryRecordEncoder(sink)

        encoder.databaseTransaction(
            sourceId = 1L,
            sourceName = "database",
            transactionId = 300L,
            parentId = 0L,
            stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
            mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            durationUs = 0L,
            statementCount = 0L,
            readCount = 0L,
            writeCount = 0L,
            mainThread = false,
        )
        encoder.database(
            sourceId = 1L,
            sourceName = "database",
            query = null,
            framework = Jhlog.DATABASE_FRAMEWORK_SQLITE,
            operation = Jhlog.DATABASE_OPERATION_QUERY,
            outcome = Jhlog.DATABASE_OUTCOME_SUCCESS,
            durationUs = 1L,
            mainThread = false,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE,
            statementFingerprint = 1L,
            resultKnown = false,
            resultKind = Jhlog.DATABASE_RESULT_UNKNOWN,
            resultCountBucket = Jhlog.DATABASE_COUNT_UNKNOWN,
            transactionId = 301L,
            statementToken = 0L,
            phaseMask = 0L,
            poolWaitUs = 0L,
            lockWaitUs = 0L,
            executeUs = 0L,
            materializeUs = 0L,
        )
        encoder.databaseTransaction(
            sourceId = 1L,
            sourceName = "database",
            transactionId = 301L,
            parentId = 300L,
            stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
            mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            durationUs = 0L,
            statementCount = 0L,
            readCount = 0L,
            writeCount = 0L,
            mainThread = false,
        )

        val first = decodeUvarints(sink.records[0])
        val third = decodeUvarints(sink.records[2])
        assertEquals(600L, first[1])
        assertEquals(0L, third[1])
        assertEquals(1L, third[4])
    }

    @Test
    fun resetSegmentStateRestartsTransactionDeltaFromZero() {
        val sink = CapturingSink()
        val encoder = DatabaseBinaryRecordEncoder(sink)

        encoder.databaseTransaction(
            sourceId = 1L,
            sourceName = "database",
            transactionId = 300L,
            parentId = 0L,
            stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
            mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            durationUs = 0L,
            statementCount = 0L,
            readCount = 0L,
            writeCount = 0L,
            mainThread = false,
        )
        encoder.resetSegmentState()
        encoder.databaseTransaction(
            sourceId = 1L,
            sourceName = "database",
            transactionId = 7L,
            parentId = 0L,
            stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
            mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            durationUs = 0L,
            statementCount = 0L,
            readCount = 0L,
            writeCount = 0L,
            mainThread = false,
        )

        assertEquals(14L, decodeUvarints(sink.records[1])[1])
    }

    private fun decodeUvarints(bytes: ByteArray): List<Long> {
        val values = ArrayList<Long>()
        var offset = 0
        while (offset < bytes.size) {
            var value = 0L
            var shift = 0
            while (true) {
                val byte = bytes[offset++].toInt() and 0xff
                value = value or ((byte and 0x7f).toLong() shl shift)
                if (byte and 0x80 == 0) break
                shift += 7
            }
            values += value
        }
        return values
    }

    private class CapturingSink : BinaryEncodingSink {
        val records = ArrayList<ByteArray>()
        private val payload = BinaryPayload()

        override fun payload(): BinaryPayload = payload.clear()

        override fun optionalSymbolId(kind: Int, value: String?): Long = 0L

        override fun defineStableSymbol(id: Long, name: String?): Long = 1L

        override fun producerContext(owner: String?): BinaryRecordContext? = null

        override fun emitDictionaryDefinition(payload: BinaryPayload) = Unit

        override fun emitControl(recordType: Int, payload: BinaryPayload) = Unit

        override fun emitSemantic(
            recordType: Int,
            attributes: Long,
            payload: BinaryPayload,
            context: BinaryRecordContext?,
            semanticEventCount: Long,
        ) {
            records += payload.copyBytes()
        }
    }
}
