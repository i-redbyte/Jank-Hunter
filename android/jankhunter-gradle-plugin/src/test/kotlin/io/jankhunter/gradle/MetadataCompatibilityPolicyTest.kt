package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class MetadataCompatibilityPolicyTest {
    @Test fun unknownSchemaIsVisibleInCompatibilityAndFailsInStrict() {
        val metadata = kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(99, 0))
        assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_VERSION,
            KotlinGeneratedMethodIndex.from(metadata).analysisStatus)
        val error = assertThrows(IllegalStateException::class.java) {
            KotlinGeneratedMethodIndex.from(metadata, JankHunterMetadataCompatibilityMode.STRICT, "example.Future")
        }
        assertTrue(error.message.orEmpty().contains("example.Future"))
        assertTrue(error.message.orEmpty().contains("99.0"))
    }

    @Test fun unsupportedVersionDoesNotOpenReader() {
        val metadata = kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(2, 4, 0))
        val index = KotlinGeneratedMethodIndex.from(metadata)
        assertEquals(KotlinMetadataAnalysisStatus.UNSUPPORTED_VERSION, index.analysisStatus)
        assertNull(index.readerId)
    }

    @Test fun corruptedPayloadFailsStrictAndAbsentJavaRemainsValid() {
        val metadata = kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(2, 3), data1 = arrayOf("broken"))
        assertThrows(IllegalStateException::class.java) {
            KotlinGeneratedMethodIndex.from(metadata, JankHunterMetadataCompatibilityMode.STRICT, "example.Broken")
        }
        JankHunterMetadataCompatibilityMode.STRICT.validate(KotlinMetadataAnalysisStatus.ABSENT, "example.Java", "")
    }
}
