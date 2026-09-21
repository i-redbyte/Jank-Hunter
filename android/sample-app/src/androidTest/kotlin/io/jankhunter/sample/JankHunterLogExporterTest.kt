package io.jankhunter.sample

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.util.zip.ZipFile
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class JankHunterLogExporterTest {
    @Test
    fun archivesOnlyJhlogAndHprofArtifacts() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val source = File(context.cacheDir, "exporter-test-source").apply {
            deleteRecursively()
            mkdirs()
        }
        val destination = File(context.cacheDir, "exporter-test-output").apply {
            deleteRecursively()
            mkdirs()
        }
        val logBytes = byteArrayOf(1, 2, 3, 4)
        val heapBytes = byteArrayOf(5, 6, 7)
        File(source, "session.jhlog").writeBytes(logBytes)
        File(source, "retained.hprof").writeBytes(heapBytes)
        File(source, "ignored.txt").writeText("ignored")

        val archive = JankHunterLogExporter(
            context,
            source,
            destination,
            captureCurrentLogPaths = { listOf(File(source, "session.jhlog").absolutePath) },
        ).createArchive()
        requireNotNull(archive)
        ZipFile(archive).use { zip ->
            val names = zip.entries().asSequence().map { it.name }.toList()
            assertEquals(listOf("retained.hprof", "session.jhlog"), names)
            assertArrayEquals(heapBytes, zip.getInputStream(zip.getEntry("retained.hprof")).readBytes())
            assertArrayEquals(logBytes, zip.getInputStream(zip.getEntry("session.jhlog")).readBytes())
        }
    }

    @Test
    fun returnsNullWhenNoArtifactsExist() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val source = File(context.cacheDir, "exporter-empty-source").apply {
            deleteRecursively()
            mkdirs()
        }
        val destination = File(context.cacheDir, "exporter-empty-output")

        assertNull(
            JankHunterLogExporter(
                context,
                source,
                destination,
                captureCurrentLogPaths = { emptyList() },
            ).createArchive(),
        )
    }

    @Test
    fun returnsNullWhenConsistentSnapshotCannotBeCaptured() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val source = File(context.cacheDir, "exporter-failed-snapshot-source").apply {
            deleteRecursively()
            mkdirs()
        }
        val destination = File(context.cacheDir, "exporter-failed-snapshot-output")
        File(source, "unsealed.jhlog").writeBytes(byteArrayOf(1, 2, 3))

        val archive = JankHunterLogExporter(
            context,
            source,
            destination,
            captureCurrentLogPaths = { null },
        ).createArchive()

        assertNull(archive)
        assertTrue(!destination.exists() || destination.listFiles().isNullOrEmpty())
    }

    @Test
    fun capturesSealedLogFrontierBeforeReadingArtifacts() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val source = File(context.cacheDir, "exporter-checkpoint-source").apply {
            deleteRecursively()
            mkdirs()
        }
        val destination = File(context.cacheDir, "exporter-checkpoint-output").apply {
            deleteRecursively()
            mkdirs()
        }
        val log = File(source, "active.jhlog").apply { writeBytes(byteArrayOf(1)) }
        var snapshotCaptured = false
        val exporter = JankHunterLogExporter(
            context = context,
            sourceDirectory = source,
            exportDirectory = destination,
            captureCurrentLogPaths = {
                log.writeBytes(byteArrayOf(1, 2, 3))
                snapshotCaptured = true
                listOf(log.absolutePath)
            },
        )

        val archive = requireNotNull(exporter.createArchive())

        assertTrue(snapshotCaptured)
        ZipFile(archive).use { zip ->
            assertArrayEquals(
                byteArrayOf(1, 2, 3),
                zip.getInputStream(zip.getEntry("active.jhlog")).readBytes(),
            )
        }
    }

    @Test
    fun excludesOpenLogThatIsNotPartOfTheSealedSnapshot() {
        val context = ApplicationProvider.getApplicationContext<SampleApplication>()
        val source = File(context.cacheDir, "exporter-frontier-source").apply {
            deleteRecursively()
            mkdirs()
        }
        val destination = File(context.cacheDir, "exporter-frontier-output")
        val sealed = File(source, "sealed.jhlog").apply { writeBytes(byteArrayOf(1)) }
        File(source, "open.jhlog").writeBytes(byteArrayOf(2))

        val archive = requireNotNull(
            JankHunterLogExporter(
                context,
                source,
                destination,
                captureCurrentLogPaths = { listOf(sealed.absolutePath) },
            ).createArchive(),
        )

        ZipFile(archive).use { zip ->
            assertEquals(listOf("sealed.jhlog"), zip.entries().asSequence().map { it.name }.toList())
        }
    }
}
