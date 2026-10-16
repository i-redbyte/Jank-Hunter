package io.jankhunter.gradle

import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class MetadataDecodeRecoveryTest {
    @Test fun collidingStringHashesCannotShareClassification() {
        MetadataReaderMetrics.reset()
        val original = RecoveryProbe::class.java.getAnnotation(kotlin.Metadata::class.java)
        fun renamed(name: String) = kotlin.metadata.jvm.Metadata(
            kind = original.kind, metadataVersion = original.metadataVersion,
            data1 = original.data1,
            data2 = original.data2.map { when (it) { "value" -> name; "getValue" -> "get$name"; else -> it } }.toTypedArray(),
            extraString = original.extraString, packageName = original.packageName, extraInt = original.extraInt,
        )
        val first = renamed("Aa")
        val second = renamed("BB")
        assertNotEquals(first.data2.toList(), second.data2.toList())
        assertEquals(java.util.Arrays.hashCode(first.data2), java.util.Arrays.hashCode(second.data2))
        val aa = KotlinGeneratedMethodIndex.from(first).origin("getAa", "()I")
        val bb = KotlinGeneratedMethodIndex.from(second).origin("getBB", "()I")
        assertEquals("kotlin_default_accessor", aa.generatedReason)
        assertEquals(aa, bb)
    }

    @Test fun temporaryBackendFailureCanRecoverForTheSamePayload() {
        MetadataReaderMetrics.reset()
        val original = RecoveryProbe::class.java.getAnnotation(kotlin.Metadata::class.java)
        val descriptor = MetadataReaderDescriptor("recovery", setOf(original.metadataVersion.take(2).joinToString(".")),
            setOf(1), 255, 1, 10, mapOf("reader.jar" to "a".repeat(64)))
        val calls = AtomicInteger()
        val testRegistry = MetadataReaderRegistry({ MetadataReaderCapabilities(listOf(descriptor)) }) {
            object : MetadataReaderBackend {
                override fun decode(metadata: kotlin.Metadata): Map<*, *> {
                    if (calls.incrementAndGet() == 1) error("temporary backend failure")
                    return mapOf("protocol" to 1, "status" to "READABLE", "known" to true, "synthetic" to false,
                        "generated" to emptyMap<String, String>(), "declared" to emptySet<String>(),
                        "delegates" to emptyMap<String, String?>())
                }
                override fun close() = Unit
            }
        }
        val field = IsolatedMetadataReader::class.java.getDeclaredField("registry").apply { isAccessible = true }
        val previous = field.get(null)
        try {
            field.set(null, testRegistry)
            assertEquals(KotlinMetadataAnalysisStatus.READER_UNAVAILABLE, KotlinGeneratedMethodIndex.from(original).analysisStatus)
            assertEquals(KotlinMetadataAnalysisStatus.READABLE, KotlinGeneratedMethodIndex.from(original).analysisStatus)
            assertEquals(2, calls.get())
        } finally {
            field.set(null, previous)
            testRegistry.close()
            MetadataReaderMetrics.reset()
        }
    }
}

private data class RecoveryProbe(val value: Int)
