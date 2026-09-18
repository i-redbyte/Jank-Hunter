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
        val expected = wireGolden("dictionary-stable.bin").copyOf()
        expected[0] = (BinaryLogWriter.DICT_STABLE_SYMBOL shl 2).toByte()
        assertArrayEquals(expected, sink.records[1])
    }

    @Test
    fun identicalNamesKeepExplicitOriginsSeparate() {
        val sink = CapturingSink()
        val encoder = BinaryDictionaryEncoder(sink, LogQualityCounters(), 16, 256)
        for (origin in SymbolOrigin.entries) {
            val id = encoder.symbolId(BinaryLogWriter.DICT_CLASS, "a", origin)
            assertEquals(origin.ordinal.toLong() + 1L, id)
            assertEquals(id, encoder.symbolId(BinaryLogWriter.DICT_CLASS, "a", origin))
            assertEquals((BinaryLogWriter.DICT_CLASS shl 2) or origin.wireValue, sink.records.last()[0].toInt())
        }
        assertEquals(4, sink.records.size)
    }

    private class CapturingSink : BinaryEncodingSink {
        var semanticEmissions = 0
        var dictionaryEmissions = 0
        val records = ArrayList<ByteArray>()
        private val payload = BinaryPayload()

        override fun payload(): BinaryPayload = payload.clear()

        override fun symbolId(kind: Int, value: String?, origin: SymbolOrigin): Long = symbolId(kind, value)

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
