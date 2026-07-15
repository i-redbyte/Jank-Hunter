package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import java.io.File
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RetainedHeapDumperTest {
    @Test
    fun writesHprofThroughExternalBinaryArtifactLifecycle() {
        val root = Files.createTempDirectory("jankhunter-heap-external").toFile()
        try {
            val fallbackDirectory = File(root, "fallback")
            val storage = ArtifactStorage(File(root, "storage"))
            val dumper = RetainedHeapDumper(
                directory = fallbackDirectory,
                binaryStorage = storage,
                minIntervalMs = 0L,
                maxDumpCount = 1,
                clock = { 1_000L },
                wallClock = { 42L },
                dumpHprof = { path -> File(path).writeBytes(ByteArray(16) { 7 }) },
            )

            val result = dumper.maybeDump("External", "Owner", 5_000L, 1L)

            assertTrue(result is RetainedHeapDumper.Result.Dumped)
            val dumped = result as RetainedHeapDumper.Result.Dumped
            assertTrue(dumped.file.absolutePath.startsWith(storage.directory.absolutePath))
            assertEquals(16L, dumped.file.length())
            assertEquals(1, storage.commits)
            assertEquals(0, storage.aborts)
            assertEquals(listOf(dumped.file.absolutePath), storage.retentionProtectedPaths)
            assertFalse("external storage must avoid creating fallback directory", fallbackDirectory.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun writesHprofThroughInjectedDumperAndAppliesSafeFileName() {
        var now = 1_000L
        val dir = tempDir()
        val paths = mutableListOf<String>()
        val dumper = RetainedHeapDumper(
            directory = dir,
            minIntervalMs = 10_000L,
            maxDumpCount = 1,
            clock = { now },
            wallClock = { 42L },
            dumpHprof = { path ->
                paths += path
                File(path).writeText("hprof")
            },
        )

        val result = dumper.maybeDump("com.example.Leaky Activity", "Owner", 5_000L, 2L)

        assertTrue(result is RetainedHeapDumper.Result.Dumped)
        val dumped = result as RetainedHeapDumper.Result.Dumped
        assertEquals("com.example.Leaky_Activity", dumped.className)
        assertEquals(5_000L, dumped.ageMs)
        assertEquals(2L, dumped.count)
        assertEquals(1, paths.size)
        assertTrue(File(paths.single()).exists())
        assertTrue(paths.single().contains("retained-42-com.example.Leaky_Activity-1.hprof"))
    }

    @Test
    fun skipsWhenIntervalOrMaxCountWouldMakeLargeRunsUnsafe() {
        var now = 1_000L
        val dumper = RetainedHeapDumper(
            directory = tempDir(),
            minIntervalMs = 10_000L,
            maxDumpCount = 2,
            clock = { now },
            wallClock = { 42L },
            dumpHprof = { path -> File(path).writeText("hprof") },
        )

        assertTrue(dumper.maybeDump("A", "Owner", 1, 1) is RetainedHeapDumper.Result.Dumped)

        now = 2_000L
        assertEquals(
            RetainedHeapDumper.Result.Skipped("min_interval"),
            dumper.maybeDump("B", "Owner", 1, 1),
        )

        now = 20_000L
        assertTrue(dumper.maybeDump("C", "Owner", 1, 1) is RetainedHeapDumper.Result.Dumped)

        now = 40_000L
        assertEquals(
            RetainedHeapDumper.Result.Skipped("max_count"),
            dumper.maybeDump("D", "Owner", 1, 1),
        )
    }

    @Test
    fun skipsWhenRetainedObjectIsTooYoungForHeapDump() {
        val dumper = RetainedHeapDumper(
            directory = tempDir(),
            minIntervalMs = 0L,
            maxDumpCount = 1,
            minRetainedAgeMs = 30_000L,
            clock = { 1_000L },
            wallClock = { 42L },
            dumpHprof = { path -> File(path).writeText("hprof") },
        )

        assertEquals(
            RetainedHeapDumper.Result.Skipped("min_age"),
            dumper.maybeDump("A", "Owner", 29_999L, 1),
        )
        assertTrue(dumper.maybeDump("A", "Owner", 30_000L, 1) is RetainedHeapDumper.Result.Dumped)
    }

    @Test
    fun reportsFailureWithoutThrowingIntoRuntimeWatcher() {
        val dumper = RetainedHeapDumper(
            directory = tempDir(),
            minIntervalMs = 0L,
            maxDumpCount = 1,
            clock = { 1_000L },
            wallClock = { 42L },
            dumpHprof = { error("boom") },
        )

        assertEquals(
            RetainedHeapDumper.Result.Failed("IllegalStateException"),
            dumper.maybeDump("A", "Owner", 1, 1),
        )
    }

    @Test
    fun failedDumpDoesNotConsumeSingleDumpSlotOrInterval() {
        var shouldFail = true
        val paths = mutableListOf<String>()
        val dumper = RetainedHeapDumper(
            directory = tempDir(),
            minIntervalMs = 60_000L,
            maxDumpCount = 1,
            clock = { 1_000L },
            wallClock = { 42L },
            dumpHprof = { path ->
                if (shouldFail) {
                    shouldFail = false
                    error("boom")
                }
                paths += path
                File(path).writeText("hprof")
            },
        )

        assertEquals(
            RetainedHeapDumper.Result.Failed("IllegalStateException"),
            dumper.maybeDump("A", "Owner", 1, 1),
        )

        val result = dumper.maybeDump("B", "Owner", 1, 1)

        assertTrue(result is RetainedHeapDumper.Result.Dumped)
        assertEquals(1, paths.size)
        assertTrue(paths.single().contains("retained-42-B-1.hprof"))
    }

    private fun tempDir(): File = Files.createTempDirectory("jankhunter-heap-dumper-test").toFile()

    private class ArtifactStorage(
        val directory: File,
    ) : JankHunterBinaryStorage {
        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE
        val retentionProtectedPaths = mutableListOf<String?>()
        var commits = 0
        var aborts = 0

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            error("not used by retained heap dumper")
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
            directory.mkdirs()
            val file = File(directory, fileName)
            return object : JankHunterBinaryArtifact {
                override val path: String = file.absolutePath

                override fun commit() {
                    commits++
                    cleanup(setOf(path))
                }

                override fun abort() {
                    aborts++
                    file.delete()
                }
            }
        }

        override fun cleanup(protectedPaths: Set<String>) {
            retentionProtectedPaths += protectedPaths
        }

        override fun listFiles(): List<String> {
            return directory
                .listFiles { file -> file.isFile }
                .orEmpty()
                .map { file -> file.absolutePath }
        }
    }
}
