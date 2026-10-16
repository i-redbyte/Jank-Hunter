package io.jankhunter.runtime

import android.content.ContextWrapper
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.nio.file.Files
import java.util.zip.ZipFile
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeSessionArchiveCaptureTest {
    @Test
    fun unavailableMultiProcessSnapshotCannotFallBackToReadingLiveFiles() {
        withRecording { graph, destination ->
            // Coordinator startup failed while another declared process may still be writing.
            graph.state.snapshotExpectedProcessCount = 2
            assertNull(graph.session.captureSessionArchiveArtifacts(destination))
            assertTrue(destination.listFiles().isNullOrEmpty())
        }
    }

    @Test
    fun inactiveCompletedRecordingCanStillBeExportedWithoutSnapshotCoordinator() {
        withRecording { graph, destination ->
            assertTrue(checkNotNull(graph.state.writer).close(1_000L))
            graph.state.writer = null
            val archives = checkNotNull(graph.session.captureSessionArchiveArtifacts(destination))
            assertEquals(1, archives.size)
            ZipFile(archives.single().archivePath).use { zip ->
                assertEquals(1, zip.entries().asSequence().count { it.name.endsWith(".jhlog") })
            }
        }
    }

    private fun withRecording(block: (RuntimeComponentGraph, File) -> Unit) {
        val workspace = Files.createTempDirectory("jh-capture-failure").toFile()
        val root = File(workspace, "jankhunter")
        val config = JankHunterConfig.builder().logDirectory(root).build()
        val writer = AsyncLogWriterFactory().open(root, config, "main")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.initContext = object : ContextWrapper(null) {
            override fun getNoBackupFilesDir(): File = File(workspace, "state")
            override fun getFilesDir(): File = workspace
        }
        graph.state.config = config
        graph.state.writer = writer
        try {
            writer.counter("capture.fixture", 1L)
            assertTrue(writer.flushBlocking(1_000L))
            assertEquals(1, root.walkTopDown().count { it.isFile && it.extension == "jhlog" })
            block(graph, File(workspace, "export"))
        } finally {
            assertTrue(writer.close(1_000L))
            workspace.deleteRecursively()
        }
    }
}
