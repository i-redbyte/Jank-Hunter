package io.jankhunter.runtime.internal.io

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.io.RandomAccessFile
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SessionArtifactLayoutArtTest {
    @Test
    fun oversizedHeapArchiveSurvivesNextSessionAndFullExportOnArt() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val root = File(context.cacheDir, "jh-oversized-retention-${System.nanoTime()}")
        val destination = File(context.cacheDir, "jh-oversized-export-${System.nanoTime()}")
        val config = JankHunterConfig.builder()
            .maxSessionLogSizeMiB(1)
            .flushIntervalMs(60_000L)
            .build()
        try {
            val first = AsyncLogWriterFactory { START_MS }.open(root, config, "main")
            first.counter("retention.art.first", 1L)
            assertTrue(first.close(timeoutMs = 5_000L))
            val session = root.listFiles { file -> file.isDirectory }
                .orEmpty().single { file -> SessionArtifactPath.parseSessionDirectoryName(file.name) != null }
            val process = session.listFiles { file -> file.isDirectory }.orEmpty().single()
            val heapDump = File(process, "retained-$START_MS-LeakedActivity-1.hprof")
            RandomAccessFile(heapDump, "rw").use { file -> file.setLength(2L * 1024L * 1024L) }

            val second = AsyncLogWriterFactory { START_MS + 1_000L }.open(root, config, "main")
            second.counter("retention.art.second", 1L)
            assertTrue(second.close(timeoutMs = 5_000L))

            val archive = root.listFiles { file -> file.isFile && file.name.endsWith(".jhlog.zip") }
                .orEmpty().single()
            ZipFile(archive).use { zip ->
                assertTrue(zip.entries().asSequence().any { entry -> entry.name.endsWith(".hprof") })
            }
            val exported = SessionArchiveExporter.export(root, destination, snapshot = null, includeHeapDumps = true)
            assertTrue(exported.isNotEmpty())
            assertTrue(exported.any { path ->
                ZipFile(path).use { zip ->
                    zip.entries().asSequence().any { entry -> entry.name.endsWith(".hprof") }
                }
            })
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun lightweightExportOnArtPreservesJhlogWithoutCopyingLargeHeapDump() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val root = File(context.cacheDir, "jh-light-export-${System.nanoTime()}")
        val destination = File(context.cacheDir, "jh-light-export-output-${System.nanoTime()}")
        try {
            val writer = AsyncLogWriterFactory { START_MS }.open(
                root,
                JankHunterConfig.builder().flushIntervalMs(60_000L).build(),
                "main",
            )
            writer.counter("light.export.art", 1L)
            assertTrue(writer.close(timeoutMs = 5_000L))
            val session = root.listFiles { file -> file.isDirectory }
                .orEmpty().single { file -> SessionArtifactPath.parseSessionDirectoryName(file.name) != null }
            val process = session.listFiles { file -> file.isDirectory }.orEmpty().single()
            val heapDump = File(process, "retained-$START_MS-LeakedActivity-1.hprof")
            RandomAccessFile(heapDump, "rw").use { file -> file.setLength(24L * 1024L * 1024L) }

            val exported = SessionArchiveExporter.export(root, destination, snapshot = null, includeHeapDumps = false)
            val archive = File(exported.single())

            assertTrue(archive.length() < 20L * 1024L * 1024L)
            assertTrue(heapDump.isFile)
            ZipFile(archive).use { zip ->
                val names = zip.entries().asSequence().map { entry -> entry.name }.toList()
                assertTrue(names.any { name -> name.endsWith(".jhlog") })
                assertTrue(names.none { name -> name.endsWith(".hprof") })
            }
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    @Test
    fun builtInStorageWritesThroughSessionAndProcessDirectories() {
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val root = File(context.cacheDir, "jh-session-layout-${System.nanoTime()}")
        val destination = File(context.cacheDir, "jh-session-layout-export-${System.nanoTime()}")
        try {
            val legacy = File(
                root,
                SessionLogName.create("2027-01-02", ByteArray(16) { 7 }, 0L, 0L),
            ).apply {
                parentFile?.mkdirs()
                writeBytes(Jhlog.FILE_MAGIC)
            }
            val writer = AsyncLogWriterFactory { START_MS }.open(
                root,
                JankHunterConfig.builder().flushIntervalMs(60_000L).build(),
                "main",
            )
            writer.counter("layout.art", 1L)
            assertTrue(writer.close(timeoutMs = 5_000L))
            assertTrue(!legacy.exists())

            val session = root.listFiles { file -> file.isDirectory }
                .orEmpty()
                .single { file -> SessionArtifactPath.parseSessionDirectoryName(file.name) != null }
            val process = session.listFiles { file -> file.isDirectory }.orEmpty().single()
            assertEquals(SessionArtifactPath.processDirectoryName(ProcessInstanceIdentity.id()), process.name)
            assertTrue(process.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
                .orEmpty().single().length() > 0L)
            val heapDumper = RetainedHeapDumper(
                directory = root,
                minIntervalMs = 0L,
                maxDumpCount = 1,
                clock = { 1L },
                wallClock = { START_MS },
                dumpHprof = { path -> File(path).writeText("hprof") },
                managedDirectoryProvider = { process },
            )
            val heapDump = heapDumper.maybeDump("LeakedActivity", "owner", 1_000L, 1L)
            assertTrue(heapDump is RetainedHeapDumper.Result.Dumped)
            assertEquals(process, (heapDump as RetainedHeapDumper.Result.Dumped).file.parentFile)

            val next = AsyncLogWriterFactory { START_MS + 1_000L }.open(
                root,
                JankHunterConfig.builder().flushIntervalMs(60_000L).build(),
                "main",
            )
            next.counter("layout.next", 1L)
            assertTrue(next.close(timeoutMs = 5_000L))
            val archive = root.listFiles { file -> file.isFile && file.name.endsWith(".jhlog.zip") }
                .orEmpty().single()
            ZipFile(archive).use { zip ->
                val entries = zip.entries().asSequence().toList()
                assertEquals(2, entries.size)
                assertTrue(entries.all { entry -> entry.method == ZipEntry.STORED })
                assertTrue(entries.any { entry -> entry.name.endsWith(".hprof") })
            }
            val exported = SessionArchiveExporter.export(root, destination, snapshot = null)
            assertEquals(1, exported.size)
            ZipFile(exported.single()).use { zip ->
                val names = zip.entries().asSequence().map { entry -> entry.name }.toList()
                assertEquals(2, names.count { name -> name.endsWith(".jhlog") })
                assertTrue(names.any { name -> name.endsWith(".hprof") })
            }
        } finally {
            root.deleteRecursively()
            destination.deleteRecursively()
        }
    }

    private companion object {
        const val START_MS = 1_800_000_000_000L
    }
}
