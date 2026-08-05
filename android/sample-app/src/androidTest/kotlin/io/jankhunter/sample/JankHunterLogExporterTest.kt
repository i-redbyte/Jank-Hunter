package io.jankhunter.sample

import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.util.zip.ZipFile
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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

        val archive = JankHunterLogExporter(context, source, destination).createArchive()
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

        assertNull(JankHunterLogExporter(context, source, destination).createArchive())
    }
}
