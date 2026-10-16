package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterLogSnapshot
import java.util.zip.ZipFile
import org.junit.Assert.assertArrayEquals
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ProcessRecordingSessionTest {
    @Test
    fun releaseWaitsForCurrentEpochAndAllowsNextRun() {
        val root = Files.createTempDirectory("jh-recording-release").toFile()
        val recording = ProcessRecordingSession()
        val factory = AsyncLogWriterFactory(recording)
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        try {
            val first = factory.open(root, config, "main")
            first.counter("release.before.seal", 1L)
            assertTrue(first.flushBlocking(5_000L))
            val firstRun = ProcessRunCohort.activeRunIds(root).single()
            recording.releaseAfterEpoch()
            assertEquals(setOf(firstRun), ProcessRunCohort.activeRunIds(root))
            assertTrue(first.close())
            assertTrue(ProcessRunCohort.activeRunIds(root).isEmpty())
            val second = factory.open(root, config, "main")
            second.counter("release.next.run", 1L)
            assertTrue(second.flushBlocking(5_000L))
            assertTrue(firstRun !in ProcessRunCohort.activeRunIds(root))
            recording.releaseAfterEpoch()
            assertTrue(second.close())
            assertTrue(ProcessRunCohort.activeRunIds(root).isEmpty())
        } finally {
            recording.close()
            root.deleteRecursively()
        }
    }

    @Test
    fun twoProcessesShareOneSessionWithOneFileEach() {
        val root = Files.createTempDirectory("jh-two-processes").toFile()
        val destination = Files.createTempDirectory("jh-two-processes-export").toFile()
        val main = ProcessRecordingSession(BinaryLogFileHeader.randomId())
        val remote = ProcessRecordingSession(BinaryLogFileHeader.randomId())
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val mainFactory = AsyncLogWriterFactory(recording = main)
        val remoteFactory = AsyncLogWriterFactory(recording = remote)
        try {
            repeat(3) { epoch ->
                val first = mainFactory.open(root, config, "main", setOf("main", "remote"))
                val second = remoteFactory.open(root, config, "remote", setOf("main", "remote"))
                try {
                    first.counter("main.$epoch", 1L)
                    second.counter("remote.$epoch", 1L)
                    assertNotNull(first.captureSnapshotBlocking(5_000L))
                    assertNotNull(second.captureSnapshotBlocking(5_000L))
                } finally {
                    first.close()
                    second.close()
                }
            }
            val files = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
            assertEquals(2, files.size)
            assertEquals(1, files.map { it.parentFile?.parentFile }.distinct().size)
            val exported = SessionArchiveExporter.export(root, destination, null)
            assertEquals(1, exported.size)
            ZipFile(exported.single()).use { zip -> assertEquals(2, zip.entries().toList().size) }
        } finally {
            main.close()
            remote.close()
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun threeLaunchesExportThreeArchivesWithHeapOnlyInNewest() {
        val root = Files.createTempDirectory("jh-three-launches").toFile()
        val destination = Files.createTempDirectory("jh-three-export").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        try {
            repeat(3) { launch ->
                ProcessRecordingSession(BinaryLogFileHeader.randomId()).use { recording ->
                    val factory = AsyncLogWriterFactory(recording = recording)
                    repeat(3) { epoch ->
                        val writer = factory.open(root, config, "main")
                        writer.counter("launch.$launch.epoch.$epoch", 1L)
                        assertTrue(writer.close())
                    }
                    val live = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
                    assertEquals(1, live.size)
                    checkNotNull(live.single().parentFile).resolve("retained-1727172000000-Activity-1.hprof").writeText("dump-$launch")
                }
            }
            val exported = SessionArchiveExporter.export(root, destination, null)
            assertEquals(3, exported.size)
            exported.forEachIndexed { index, path ->
                ZipFile(path).use { zip ->
                    val entries = zip.entries().toList()
                    assertEquals(1, entries.count { it.name.endsWith(".jhlog") })
                    assertEquals(if (index == 2) 1 else 0, entries.count { it.name.endsWith(".hprof") })
                }
            }
            root.listFiles().orEmpty().filter { it.name.endsWith(".jhlog.zip") }.forEach { file ->
                ZipFile(file).use { zip -> assertTrue(zip.entries().toList().none { it.name.endsWith(".hprof") }) }
            }
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun exportedFrontierDoesNotIncludeEventsWrittenAfterCapture() {
        val root = Files.createTempDirectory("jh-frontier-root").toFile()
        val destination = Files.createTempDirectory("jh-frontier-export").toFile()
        val recording = ProcessRecordingSession()
        val factory = AsyncLogWriterFactory(recording = recording)
        val writer = factory.open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        try {
            writer.counter("before.snapshot", 1L)
            val capture = checkNotNull(writer.captureSnapshotBlocking(5_000L))
            val path = capture.logPaths.single()
            val limit = capture.logByteLimits.single()
            val expected = java.io.File(path).inputStream().use { it.readBytes().copyOf(limit.toInt()) }
            writer.counter("after.snapshot", 2L)
            assertTrue(writer.flushBlocking(5_000L))
            val exported = SessionArchiveExporter.export(root, destination,
                JankHunterLogSnapshot(capture.capturedAtMs, capture.logPaths, logByteLimits = capture.logByteLimits))
            ZipFile(exported.single()).use { zip ->
                val entries = zip.entries().toList().filter { it.name.endsWith(".jhlog") }
                assertEquals(1, entries.size)
                assertArrayEquals(expected, zip.getInputStream(entries.single()).use { it.readBytes() })
            }
        } finally {
            writer.close()
            recording.close()
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun reconfigurationAndSnapshotsKeepOnePhysicalFileForTheProcess() {
        val root = Files.createTempDirectory("jh-one-process").toFile()
        val recording = ProcessRecordingSession()
        val factory = AsyncLogWriterFactory(recording = recording)
        val config = JankHunterConfig.builder().autoStartCollectors(false).mainProcessOnly(true).build()
        try {
            var firstPath: String? = null
            repeat(3) { epoch ->
                val writer = factory.open(root, config, "main")
                try {
                    writer.counter("epoch.$epoch", 1L)
                    val snapshot = writer.captureSnapshotBlocking(5_000L)
                    assertNotNull("snapshot $epoch", snapshot)
                    val files = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
                    assertEquals("one physical file, including after a snapshot", 1, files.size)
                    val path = files.single().absolutePath
                    if (firstPath == null) firstPath = path else assertEquals(firstPath, path)
                    assertEquals(listOf(path), checkNotNull(snapshot).logPaths)
                } finally {
                    assertTrue(writer.close())
                }
            }
            assertEquals(1, root.walkTopDown().count { it.isFile && it.extension == "jhlog" })
            assertEquals(0, root.listFiles().orEmpty().count { it.name.endsWith(".jhlog.zip") })
        } finally {
            recording.close()
            root.deleteRecursively()
        }
    }
}
