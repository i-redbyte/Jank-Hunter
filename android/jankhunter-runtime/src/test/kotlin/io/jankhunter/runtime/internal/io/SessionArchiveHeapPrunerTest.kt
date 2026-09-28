package io.jankhunter.runtime.internal.io

import java.io.File
import java.nio.file.Files
import java.util.zip.CRC32
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionArchiveHeapPrunerTest {
    @Test
    fun dropsHistoricalHeapAndPreservesLogBytes() = withArchive { root, archive ->
        val failures = pruneHistoricalArchiveHeapDumps(root, CURRENT) { temporary, _ ->
            ZipFile(temporary).use { zip ->
                zip.size() == 1 && zip.getInputStream(zip.getEntry(LOG)).use { it.readBytes() }.contentEquals(DATA)
            }
        }
        assertEquals(0L, failures)
        ZipFile(archive).use { zip ->
            assertEquals(1, zip.size())
            assertArrayEquals(DATA, zip.getInputStream(zip.getEntry(LOG)).use { it.readBytes() })
        }
    }

    @Test
    fun failedVerificationKeepsOriginalArchiveAndRemovesTemporaryCopy() = withArchive { root, archive ->
        val before = archive.readBytes()
        assertEquals(1L, pruneHistoricalArchiveHeapDumps(root, CURRENT) { _, _ -> false })
        assertArrayEquals(before, archive.readBytes())
        assertFalse(root.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
    }

    @Test
    fun unknownHeapPathIsNotTreatedAsSdkArtifact() {
        val root = Files.createTempDirectory("jh-heap-pruner-unknown").toFile()
        try {
            val archive = archive(root, "../retained-1-Activity-1.hprof")
            val before = archive.readBytes()
            var verified = false
            assertEquals(0L, pruneHistoricalArchiveHeapDumps(root, CURRENT) { _, _ -> verified = true; true })
            assertFalse(verified)
            assertArrayEquals(before, archive.readBytes())
        } finally { root.deleteRecursively() }
    }

    private fun withArchive(block: (File, File) -> Unit) {
        val root = Files.createTempDirectory("jh-heap-pruner").toFile()
        try { block(root, archive(root, "$PROCESS/retained-1-Activity-1.hprof")) }
        finally { root.deleteRecursively() }
    }

    private fun archive(root: File, heapPath: String): File {
        val file = File(root, "2026-09-24T10-00-00.000Z_0_$RUN.jhlog.zip")
        ZipOutputStream(file.outputStream()).use { output ->
            for (name in listOf(LOG, heapPath)) {
                output.putNextEntry(ZipEntry(name).apply {
                    method = ZipEntry.STORED
                    size = DATA.size.toLong()
                    compressedSize = size
                    crc = CRC32().apply { update(DATA) }.value
                })
                output.write(DATA)
                output.closeEntry()
            }
        }
        assertTrue(file.isFile)
        return file
    }

    private companion object {
        const val RUN = "00112233445566778899aabbccddeeff"
        const val CURRENT = "10112233445566778899aabbccddeeff"
        const val PROCESS = "20112233445566778899aabbccddeeff"
        const val LOG = "$PROCESS/jh-session-log.2026-09-24.$RUN.0.jhlog"
        val DATA = byteArrayOf(1, 2, 3, 4)
    }
}
