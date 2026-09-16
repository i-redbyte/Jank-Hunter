package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class ContextTrafficWireTest {
    @Test
    fun manualContextEncodesExplicitlyUnknownTrafficProvenance() {
        val sink = CapturingSink()
        SessionBinaryRecordEncoder(sink).deviceContext(
            0, 0, 0L, 0, 0, false, false, false, 0L, 0L, 0L, 0L, 0L, false, true,
        )
        assertEquals("UID and RX/TX known flags must survive the Android encoder", 12, sink.record.size)
        assertEquals(0, sink.record[10].toInt())
        assertEquals(0, sink.record[11].toInt())
    }

    @Test
    fun encoderPreservesIndependentKnownFlagsAndUidZero() {
        for (flags in 0..3) {
            val sink = CapturingSink()
            SessionBinaryRecordEncoder(sink).deviceContext(
                0, 0, 0L, 0, 0, false, false, false, 0L, 0L, 0L, 0L, 0L, false, true,
                trafficUidPlusOne = 1L, trafficKnownFlags = flags,
            )
            assertEquals(12, sink.record.size)
            assertEquals(1, sink.record[10].toInt())
            assertEquals(flags, sink.record[11].toInt())
        }
    }

    private class CapturingSink : BinaryEncodingSink {
        lateinit var record: ByteArray
        private val payload = BinaryPayload()

        override fun payload(): BinaryPayload = payload.clear()

        override fun optionalSymbolId(kind: Int, value: String?): Long = 0L

        override fun defineStableSymbol(id: Long, name: String?): Long = id

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
            record = payload.copyBytes()
        }
    }
}
