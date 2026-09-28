package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LegacyFlatArtifactCleanerTest {
    @Test
    fun deletesOnlyProvenManagedFlatArtifactsAndIsIdempotent() {
        val root = tempDir()
        try {
            val current = File(root, SessionLogName.create(DATE, RUN_ID, 0L, 0L)).apply {
                writeBytes(Jhlog.FILE_MAGIC)
            }
            val readableLegacy = File(root, SessionLogName.create(DATE, OLD_RUN_ID, 1L, 0L)).apply {
                writeBytes(Jhlog.FILE_MAGIC.copyOf().also { magic -> magic[8] = 5; magic[9] = 0; magic[10] = 0 })
            }
            val managedHeap = File(root, "retained-42-LeakedActivity-1.hprof").apply { writeText("heap") }
            val badMagic = File(root, SessionLogName.create(DATE, OTHER_RUN_ID, 2L, 0L)).apply { writeText("user") }
            val unrelatedJhlog = File(root, "application.jhlog").apply { writeText("user") }
            val unrelatedHeap = File(root, "memory.hprof").apply { writeText("user") }
            val ambiguousHeap = File(root, "retained-manual.hprof").apply { writeText("user") }
            val archive = File(root, "report.jhlog.zip").apply { writeText("zip") }
            val directory = File(root, "retained-42-LeakedActivity-2.hprof").apply { mkdirs() }

            val first = LegacyFlatArtifactCleaner.clean(root)
            val second = LegacyFlatArtifactCleaner.clean(root)

            assertEquals(3L, first.deleted)
            assertEquals(0L, first.failed)
            assertEquals(0L, second.deleted)
            assertFalse(current.exists())
            assertFalse(readableLegacy.exists())
            assertFalse(managedHeap.exists())
            assertTrue(badMagic.exists())
            assertTrue(unrelatedJhlog.exists())
            assertTrue(unrelatedHeap.exists())
            assertTrue(ambiguousHeap.exists())
            assertTrue(archive.exists())
            assertTrue(directory.isDirectory)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun preservesActivelyLeasedFlatJhlog() {
        val root = tempDir()
        try {
            val allocation = SessionLogAllocator.reserve(root, DATE, RUN_ID, 0L)
            val file = File(root, allocation.fileName).apply { writeBytes(Jhlog.FILE_MAGIC) }
            allocation.updateProtectedPath(file.absolutePath)
            try {
                val result = LegacyFlatArtifactCleaner.clean(root)

                assertEquals(1L, result.protected)
                assertTrue(file.exists())
            } finally {
                allocation.close()
            }
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun liveCohortDefersManagedHeapDumpCleanup() {
        val root = tempDir()
        try {
            val heap = File(root, "retained-42-LeakedActivity-1.hprof").apply { writeText("heap") }
            ProcessRunCohort.join(root, DATE, startedAtUnixMs = 42L).use {
                val result = LegacyFlatArtifactCleaner.clean(root)

                assertEquals(1L, result.protected)
                assertTrue(heap.exists())
            }

            assertEquals(1L, LegacyFlatArtifactCleaner.clean(root).deleted)
            assertFalse(heap.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    private fun tempDir(): File = Files.createTempDirectory("jankhunter-legacy-cleanup").toFile()

    private companion object {
        const val DATE = "2027-01-02"
        val RUN_ID = ByteArray(16) { index -> (index + 1).toByte() }
        val OLD_RUN_ID = ByteArray(16) { index -> (index + 33).toByte() }
        val OTHER_RUN_ID = ByteArray(16) { index -> (index + 65).toByte() }
    }
}
