package io.jankhunter.gradle

import org.junit.Assert.assertThrows
import org.junit.Test

class MetadataStrictModeInstrumentationTest {
    @Test fun strictModeFailsBeforeVisitorEmitsHooksForUnsupportedMetadata() {
        val metadata = kotlin.metadata.jvm.Metadata(kind = 1, metadataVersion = intArrayOf(99, 0))
        assertThrows(IllegalStateException::class.java) {
            KotlinGeneratedMethodIndex.from(
                metadata,
                JankHunterMetadataCompatibilityMode.STRICT,
                "example.Unsupported",
            )
        }
    }
}
