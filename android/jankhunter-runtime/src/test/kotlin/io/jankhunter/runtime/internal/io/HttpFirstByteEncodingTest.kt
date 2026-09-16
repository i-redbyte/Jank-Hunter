package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterHttpEvent
import org.junit.Assert.assertEquals
import org.junit.Test

class HttpFirstByteEncodingTest {
    @Test
    fun invalidPublicTimingsBecomeUnknownInsteadOfClampedExactMeasurements() {
        val marker = Jhlog.FLAG_HTTP_TTFB_OBSERVED
        val known = Jhlog.FLAG_HTTP_TTFB_KNOWN
        for (value in listOf(-1L, 101L)) {
            val sink = encode(value, marker or known)
            assertEquals("invalid interval retained exactness", 0L, sink.flags and known)
            assertEquals(0L, ttfb(sink.bytes))
        }
        assertEquals(0L, ttfb(encode(50L, marker).bytes))
        assertEquals(0L, encode(50L, known).flags and known)
    }

    @Test
    fun actualZeroAndLegacyValueRemainDistinguishable() {
        val flags = Jhlog.FLAG_HTTP_TTFB_OBSERVED or Jhlog.FLAG_HTTP_TTFB_KNOWN
        val exact = encode(0L, flags)
        assertEquals(flags, exact.flags)
        assertEquals(0L, ttfb(exact.bytes))
        val legacy = encode(50L, 0L)
        assertEquals(0L, legacy.flags)
        assertEquals(50L, ttfb(legacy.bytes))
    }

    private fun encode(value: Long, flags: Long): Sink {
        val sink = Sink()
        val event = JankHunterHttpEvent(null, "GET /test", null,
            100L, 0L, 0L, 0L, 0L, 0L, value, 0L,
            200, 0, 0, JankHunterHttpEvent.PROTOCOL_HTTP_1_1,
            0L, 0L, 1, 0, 0, 0, 0, 0, 0, flags)
        NetworkBinaryRecordEncoder(sink).http(null, "GET /test", event, flags)
        return sink
    }

    private fun ttfb(bytes: ByteArray): Long {
        var offset = 0
        var result = 0L
        repeat(10) {
            result = 0L
            var shift = 0
            do {
                val next = bytes[offset++].toInt() and 255
                result = result or ((next and 127).toLong() shl shift)
                shift += 7
            } while (next and 128 != 0)
        }
        return result
    }

    private class Sink : BinaryEncodingSink {
        var flags = 0L
        lateinit var bytes: ByteArray
        private val payload = BinaryPayload()
        override fun payload(): BinaryPayload = payload.clear()
        override fun optionalSymbolId(kind: Int, value: String?): Long = 0L
        override fun defineStableSymbol(id: Long, name: String?): Long = id
        override fun producerContext(owner: String?): BinaryRecordContext? = null
        override fun emitDictionaryDefinition(payload: BinaryPayload) = Unit
        override fun emitControl(recordType: Int, payload: BinaryPayload) = Unit
        override fun emitSemantic(recordType: Int, attributes: Long, payload: BinaryPayload,
            context: BinaryRecordContext?, semanticEventCount: Long) {
            flags = attributes
            bytes = payload.copyBytes()
        }
    }
}
