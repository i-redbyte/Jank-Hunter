package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncLogSessionFactoryTest {
    @Test
    fun externalStorageCanOnlyTightenJankHunterLogLimit() {
        val config = JankHunterConfig.builder()
            .maxSessionLogSizeMiB(50)
            .build()

        assertEquals(20L * MIB, effectiveArchiveLimitBytes(config, LimitOnlyStorage(20L * MIB)))
        assertEquals(50L * MIB, effectiveArchiveLimitBytes(config, LimitOnlyStorage(Long.MAX_VALUE)))
        assertEquals(50L * MIB, effectiveArchiveLimitBytes(config, null))
    }

    @Test
    fun processScopeFingerprintIsOrderIndependentAndLengthDelimited() {
        val first = AsyncLogSessionFactory.processScopeFingerprint(
            linkedSetOf("com.example:sync", "com.example"),
        )
        val reordered = AsyncLogSessionFactory.processScopeFingerprint(
            linkedSetOf("com.example", "com.example:sync"),
        )
        val ambiguousConcatenation = AsyncLogSessionFactory.processScopeFingerprint(
            linkedSetOf("com.example:s", "ynccom.example"),
        )

        assertArrayEquals(first, reordered)
        assertFalse(first.contentEquals(ambiguousConcatenation))
        assertTrue(AsyncLogSessionFactory.processScopeFingerprint(emptySet()).isEmpty())
    }

    private class LimitOnlyStorage(
        override val archivesSizeLimitBytes: Long,
    ) : JankHunterBinaryStorage {
        override val fileSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter = error("not used")

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("not used")

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = emptyList()
    }

    private companion object {
        const val MIB = 1024L * 1024L
    }
}
