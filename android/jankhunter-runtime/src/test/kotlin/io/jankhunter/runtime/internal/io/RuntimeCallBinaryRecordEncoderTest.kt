package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertArrayEquals
import org.junit.Test

class RuntimeCallBinaryRecordEncoderTest {
    @Test
    fun separatesDenseEdgeIdsFromDefinitionBits() {
        val sink = CapturingSink()
        val encoder = RuntimeCallBinaryRecordEncoder(sink)
        val batch = RuntimeCallBatch(4).apply {
            add(null, 1L, "caller", 10L, 2L, "callee-a", 1L, 0L, 0L)
            add(null, 1L, "caller", 10L, 2L, "callee-a", 1L, 0L, 0L)
            add(null, 1L, "caller", 20L, 3L, "callee-b", 1L, 0L, 0L)
            add(null, 1L, "caller", 20L, 3L, "callee-b", 1L, 0L, 0L)
        }

        encoder.runtimeCalls(batch)

        val bytes = sink.record
        assertEquals(9, bytes[0].toInt() and 0xff)
        assertEquals(0b0101, bytes[1].toInt() and 0xff)
        assertArrayEquals(wireGolden("runtime-edge-columnar.bin"), bytes)
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
