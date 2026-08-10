package io.jankhunter.plugin.execution

import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterLogDiscoveryTest {
    @Test
    fun sortsCanonicalLogsByDateAndNumericIndex() = withTempDirectory { directory ->
        File(directory, "jh-session-log.2026-08-07.9.jhlog").writeText("nine")
        File(directory, "jh-session-log.2026-08-07.10.jhlog").writeText("ten")
        File(directory, "jh-session-log.2026-08-06.99.jhlog").writeText("old")

        val snapshot = JankHunterLogDiscovery.scan(directory, emptySet())

        assertEquals(
            listOf(
                "jh-session-log.2026-08-07.10.jhlog",
                "jh-session-log.2026-08-07.9.jhlog",
                "jh-session-log.2026-08-06.99.jhlog",
            ),
            snapshot.logs.map { it.file.name },
        )
    }

    @Test
    fun marksFingerprintAsProcessedAndFindsHeapCandidates() = withTempDirectory { directory ->
        val log = File(directory, "jh-session-log.2026-08-07.1.jhlog").apply { writeText("log") }
        val heap = File(directory, "retained.hprof").apply { writeText("heap") }
        val fingerprint = JankHunterLogDiscovery.fingerprint(log)

        val snapshot = JankHunterLogDiscovery.scan(directory, setOf(fingerprint))

        assertTrue(snapshot.logs.single().processed)
        assertEquals(heap, snapshot.heapCandidates.single())
        assertFalse(JankHunterLogDiscovery.scan(File(directory, "missing"), emptySet()).logs.isNotEmpty())
    }

    @Test
    fun sanitizesSourceDirectoryName() {
        assertEquals("Cloud-debug-logs", JankHunterLogDiscovery.sourceName(File("Cloud debug logs")))
        assertEquals("logs", JankHunterLogDiscovery.sourceName(null))
    }

    @Test
    fun appliesLimitAfterFilteringSupportedFiles() = withTempDirectory { directory ->
        val unrelated = File(directory, "notes.txt").apply { writeText("ignore") }
        val log = File(directory, "session.jhlog").apply { writeText("log") }
        val heap = File(directory, "heap.hprof").apply { writeText("heap") }

        assertEquals(
            listOf(log),
            JankHunterLogDiscovery.supportedFiles(sequenceOf(unrelated, log, heap), limit = 1),
        )
    }

    private fun withTempDirectory(block: (File) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-log-discovery").toFile()
        try {
            block(directory)
        } finally {
            directory.deleteRecursively()
        }
    }
}
