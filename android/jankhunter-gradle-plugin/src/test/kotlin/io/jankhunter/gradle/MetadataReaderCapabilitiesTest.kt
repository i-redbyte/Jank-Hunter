package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test

class MetadataReaderCapabilitiesTest {
    private val current = MetadataReaderDescriptor(
        "current", setOf("1.1", "1.4", "1.9", "2.0", "2.1", "2.2", "2.3"),
        (1..5).toSet(), 0xff, 1, 10, mapOf("reader.jar" to "a".repeat(64)),
    )

    @Test fun selectsByClassFormatRatherThanCompilerPatch() {
        val capabilities = MetadataReaderCapabilities(listOf(current))
        assertEquals("current", capabilities.select(intArrayOf(1, 9, 999), 1, 48).reader?.id)
        assertEquals("current", capabilities.select(intArrayOf(2, 3, 0), 1, 8).reader?.id)
        assertEquals("current", capabilities.select(intArrayOf(1, 1, 16), 2, 0).reader?.id)
        assertNull(capabilities.select(intArrayOf(2, 4, 0), 1, 48).reader)
    }

    @Test fun firFlagOnMetadataTwoIsRejectedBeforeReaderLoad() {
        val capabilities = MetadataReaderCapabilities(listOf(current))
        assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_FLAGS,
            capabilities.select(intArrayOf(2, 3, 0), 1, 64).status)
        assertNull(capabilities.select(intArrayOf(2, 3, 0), 1, 64).reader)
    }

    @Test fun malformedKindsAndUnknownFlagsCannotSelectAReader() {
        val capabilities = MetadataReaderCapabilities(listOf(current))
        assertEquals(KotlinMetadataAnalysisStatus.UNREADABLE, capabilities.select(intArrayOf(2), 1, 0).status)
        assertEquals(KotlinMetadataAnalysisStatus.UNREADABLE, capabilities.select(intArrayOf(2, -1), 1, 0).status)
        assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_KIND, capabilities.select(intArrayOf(2, 3), 99, 0).status)
        assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_FLAGS, capabilities.select(intArrayOf(2, 3), 1, 256).status)
        assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_FLAGS, capabilities.select(intArrayOf(2, 3), 1, 64).status)
    }

    @Test fun compatibleBackendPriorityIsDeterministicAndAmbiguityIsRejected() {
        val preferred = current.copy(id = "preferred", priority = 20)
        assertEquals("preferred", MetadataReaderCapabilities(listOf(current, preferred)).select(intArrayOf(2, 3), 1, 0).reader?.id)
        assertThrows(IllegalArgumentException::class.java) { MetadataReaderCapabilities(listOf(current, current.copy(id = "duplicate-priority"))) }
        assertThrows(IllegalArgumentException::class.java) { MetadataReaderCapabilities(listOf(current, current)) }
    }

    @Test fun selectorCacheNeverExceedsSixtyFourDistinctHeaders() {
        val capabilities = MetadataReaderCapabilities(listOf(current))
        repeat(80) { index ->
            val selection = capabilities.select(intArrayOf(2, index % 4, 0), 1 + (index % 5), index and 0x3F)
            assertEquals("current", selection.reader?.id)
        }
        val field = MetadataReaderCapabilities::class.java.getDeclaredField("selections").apply { isAccessible = true }
        assertEquals(64, (field.get(capabilities) as Map<*, *>).size)
        assertEquals("current", capabilities.select(intArrayOf(2, 3), 5, 0).reader?.id)
        assertEquals(64, (field.get(capabilities) as Map<*, *>).size)
    }

    @Test fun invalidResourceAndProtocolContractsAreRejected() {
        assertThrows(IllegalArgumentException::class.java) { MetadataReaderCapabilities(listOf(current.copy(protocol = 2))) }
        assertThrows(IllegalArgumentException::class.java) { MetadataReaderCapabilities(listOf(current.copy(jars = mapOf("../reader.jar" to "a".repeat(64))))) }
        assertThrows(IllegalArgumentException::class.java) { MetadataReaderCapabilities(listOf(current.copy(jars = mapOf("reader.jar" to "not-a-digest")))) }
        assertThrows(IllegalArgumentException::class.java) { MetadataReaderCapabilities((1..17).map { current.copy(id = "$it", priority = it) }) }
    }
}
