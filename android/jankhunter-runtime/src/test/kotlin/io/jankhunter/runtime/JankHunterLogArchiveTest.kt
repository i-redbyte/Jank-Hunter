package io.jankhunter.runtime

import java.nio.file.Files
import java.util.zip.ZipFile
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterLogArchiveTest {
    @Test
    fun writesAllProcessSegmentsIntoOneAtomicArchive() {
        val directory = Files.createTempDirectory("jankhunter-log-archive").toFile()
        try {
            val first = directory.resolve("jh-session-log.2026-08-17.00112233445566778899aabbccddeeff.0.jhlog")
            val second = directory.resolve("jh-session-log.2026-08-17.00112233445566778899aabbccddeeff.0-1.jhlog")
            first.writeBytes(byteArrayOf(1, 2, 3))
            second.writeBytes(byteArrayOf(4, 5, 6, 7))
            val destination = directory.resolve("snapshot.zip")

            val archive = JankHunterLogArchiveWriter.write(
                destination,
                JankHunterLogSnapshot(
                    capturedAtMs = 42L,
                    logPaths = listOf(second.path, first.path),
                    processCount = 2,
                    captureSkewMs = 7L,
                ),
            )

            assertEquals(destination.absolutePath, archive.archivePath)
            assertEquals(2, archive.logCount)
            assertEquals(2, archive.processCount)
            assertEquals(7L, archive.captureSkewMs)
            assertTrue(archive.archiveBytes > first.length() + second.length())
            ZipFile(destination).use { zip ->
                val entries = zip.entries().asSequence().toList()
                assertEquals(listOf(first.name, second.name), entries.map { entry -> entry.name })
                assertArrayEquals(first.readBytes(), zip.getInputStream(entries[0]).use { it.readBytes() })
                assertArrayEquals(second.readBytes(), zip.getInputStream(entries[1]).use { it.readBytes() })
            }
            assertFalse(directory.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun neverOverwritesExistingDestination() {
        val directory = Files.createTempDirectory("jankhunter-log-archive-existing").toFile()
        try {
            val source = directory.resolve("session.jhlog").apply { writeBytes(byteArrayOf(1)) }
            val destination = directory.resolve("snapshot.zip").apply { writeBytes(byteArrayOf(9)) }
            val snapshot = JankHunterLogSnapshot(1L, listOf(source.path))

            assertThrows(IllegalArgumentException::class.java) {
                JankHunterLogArchiveWriter.write(destination, snapshot)
            }
            assertArrayEquals(byteArrayOf(9), destination.readBytes())
            assertFalse(directory.listFiles().orEmpty().any { it.name.endsWith(".tmp") })
        } finally {
            directory.deleteRecursively()
        }
    }
}
