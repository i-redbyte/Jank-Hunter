package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertArrayEquals
import org.junit.Test

class BinaryDictionaryEncoderTest {
    @Test
    fun dictionaryDefinitionBypassesSemanticEmissionPath() {
        val sink = CapturingSink()
        val encoder = BinaryDictionaryEncoder(
            sink = sink,
            quality = LogQualityCounters(),
            maxDictionaryEntries = 16,
            maxDictionaryValueBytes = 256,
        )

        encoder.symbolId(BinaryLogWriter.DICT_GENERIC, "io.jankhunter.Example.call")

        assertEquals(0, sink.semanticEmissions)
        assertEquals(1, sink.dictionaryEmissions)
    }

    @Test
    fun stableAliasesAreImplicitInDictionaryWireRecords() {
        val sink = CapturingSink()
        val encoder = BinaryDictionaryEncoder(
            sink = sink,
            quality = LogQualityCounters(),
            maxDictionaryEntries = 16,
            maxDictionaryValueBytes = 256,
        )

        encoder.symbolId(BinaryLogWriter.DICT_GENERIC, "x")
        encoder.defineStableSymbol(0x0102_0304_0506_0708L, "y")

        assertArrayEquals(byteArrayOf(0, 1, 0, 1, 'x'.code.toByte()), sink.records[0])
        assertArrayEquals(wireGolden("dictionary-stable.bin"), sink.records[1])
    }

    private class CapturingSink : BinaryEncodingSink {
        var semanticEmissions = 0
        var dictionaryEmissions = 0
        val records = ArrayList<ByteArray>()
        private val payload = BinaryPayload()

        override fun payload(): BinaryPayload = payload.clear()

        override fun optionalSymbolId(kind: Int, value: String?): Long = 0L

        override fun defineStableSymbol(id: Long, name: String?): Long = 0L

        override fun producerContext(owner: String?): BinaryRecordContext? = null

        override fun emitDictionaryDefinition(payload: BinaryPayload) {
            dictionaryEmissions++
            records += payload.copyBytes()
        }

        override fun emitControl(recordType: Int, payload: BinaryPayload) = Unit

        override fun emitSemantic(
            recordType: Int,
            attributes: Long,
            payload: BinaryPayload,
            context: BinaryRecordContext?,
            semanticEventCount: Long,
        ) {
            semanticEmissions++
        }
    }
}
