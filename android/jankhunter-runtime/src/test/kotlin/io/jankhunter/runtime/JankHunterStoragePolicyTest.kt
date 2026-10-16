package io.jankhunter.runtime

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterStoragePolicyTest {
    @Test
    fun freezesExtensionsAndPreservesPolicyAcrossReconfiguration() {
        val allowed = mutableSetOf("jhlog", "hprof")
        val exempt = mutableSetOf("hprof")
        val policy = JankHunterStoragePolicy(File("/tmp/jh-policy"), 50_000_000L, 70_000_000L,
            allowed, exempt, 32_768, true)
        allowed.clear()
        exempt.clear()
        assertTrue(policy.allowsExtension("jhlog"))
        assertTrue(policy.allowsExtension("hprof"))
        assertFalse(policy.allowsExtension("zip"))
        assertEquals(50_000_000L, policy.fileLimitBytes("jhlog"))
        assertEquals(Long.MAX_VALUE, policy.fileLimitBytes("hprof"))
        val config = JankHunterConfig.builder().storagePolicy(policy).build()
        assertSame(policy, config.toBuilder().build().storagePolicy())
    }

    @Test
    fun preservesSafeHostExtensionNamesAndHasValueEquality() {
        val extensions = setOf("jhlog", "hprof", "jhlog.zip", "heap-dump")
        val first = JankHunterStoragePolicy(File("/tmp/jh-policy"), 50L, 70L, extensions, setOf("hprof"), 32, true)
        val second = JankHunterStoragePolicy(File("/tmp/jh-policy"), 50L, 70L, extensions, setOf("hprof"), 32, true)
        assertTrue(first.allowsExtension("jhlog.zip"))
        assertTrue(first.allowsExtension("heap-dump"))
        assertEquals(first, second)
        assertEquals(first.hashCode(), second.hashCode())
    }

    @Test
    fun effectiveExtensionsAreNotNormalizedTwice() {
        // Shared Logger removes one leading dot: raw "..hprof" becomes effective ".hprof".
        val allowed = mutableSetOf("jhlog", ".hprof")
        val exempt = mutableSetOf(".hprof")
        val policy = JankHunterStoragePolicy(File("/tmp/jh-policy"), 50L, 70L, allowed, exempt, 32, true)
        allowed.clear()
        exempt.clear()
        assertEquals(setOf("jhlog", ".hprof"), policy.fileExtensions)
        assertEquals(setOf(".hprof"), policy.artifactFileSizeLimitExemptExtensions)
        assertFalse(policy.allowsExtension("hprof"))
        assertEquals(50L, policy.fileLimitBytes("hprof"))
        assertTrue(policy.allowsExtension("jhlog"))
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsCompetingStorageOwners() {
        val storage = object : JankHunterBinaryStorage {
            override val fileSizeLimitBytes = Long.MAX_VALUE
            override val archivesSizeLimitBytes = Long.MAX_VALUE
            override fun openWriter(fileName: String): JankHunterBinaryWriter = error("unused")
            override fun createArtifact(fileName: String): JankHunterBinaryArtifact = error("unused")
            override fun cleanup(protectedPaths: Set<String>) = Unit
            override fun listFiles(): List<String> = emptyList()
        }
        val policy = JankHunterStoragePolicy(File("/tmp/jh-policy"), 50L, 70L, setOf("jhlog"), emptySet(), 32, true)
        JankHunterConfig.builder().binaryStorage(storage).storagePolicy(policy).build()
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsNonPositiveLimits() {
        JankHunterStoragePolicy(File("/tmp/jh-policy"), 0L, 1L, setOf("jhlog"), emptySet(), 1, false)
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsNonPositiveBuffer() {
        JankHunterStoragePolicy(File("/tmp/jh-policy"), 1L, 1L, setOf("jhlog"), emptySet(), 0, false)
    }
}
