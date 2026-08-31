package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterInitDiagnosticsTest {
    @After
    fun tearDown() {
        JankHunter.shutdown()
    }

    @Test
    fun initRecordsMissingContext() {
        val previousAttempts = JankHunter.initDiagnostics().attempts

        JankHunter.init(null)

        val diagnostics = JankHunter.initDiagnostics()
        assertEquals("missing_context", diagnostics.status)
        assertEquals(previousAttempts + 1, diagnostics.attempts)
        assertNull(diagnostics.failureClass)
        assertNull(diagnostics.failureMessage)
        assertFalse(JankHunter.isStarted())
    }

    @Test
    fun generatedAutoInitDoesNotConsumeOneShotGateWithoutContext() {
        val previousAttempts = JankHunter.initDiagnostics().attempts

        JankHunter.autoInit(null)
        JankHunter.autoInit(null)

        val diagnostics = JankHunter.initDiagnostics()
        assertEquals(previousAttempts, diagnostics.attempts)
    }

    @Test
    fun lazyWriterFailureCleansRuntimeAndKeepsInitForRetry() {
        val filesDir = File(tempDir(), "files").apply {
            writeText("not a directory")
        }
        val context = FailingLogDirectoryContext(filesDir)

        JankHunter.init(context, JankHunterConfig.builder().autoStartCollectors(false).build())

        val diagnostics = awaitWriterFailure()
        assertEquals("failed", diagnostics.status)
        assertEquals(diagnostics.toString(), "IOException", diagnostics.failureClass)
        assertTrue(diagnostics.failureMessage.orEmpty().contains("Cannot create Jank Hunter metadata directory"))
        assertEquals("com.example", diagnostics.processName)
        assertTrue(diagnostics.logDirectory.orEmpty().endsWith("files/jankhunter"))
        assertNotNull(diagnostics.failureMessage)
        assertFalse(JankHunter.isStarted())

        assertTrue(filesDir.delete())
        assertTrue(filesDir.mkdirs())
        assertTrue(JankHunter.setRuntimeEnabled(true, "writer_recovery"))
        assertTrue(JankHunter.isStarted())
        assertEquals("started", JankHunter.initDiagnostics().status)
    }

    @Test
    fun initCanBindRuntimeDisabledConfigForFeatureFlags() {
        val filesDir = File(tempDir(), "files").apply { mkdirs() }
        val context = FailingLogDirectoryContext(filesDir)

        JankHunter.init(
            context,
            JankHunterConfig.builder()
                .runtimeEnabled(false)
                .autoStartCollectors(false)
                .build(),
        )

        val diagnostics = JankHunter.initDiagnostics()
        assertEquals("runtime_disabled", diagnostics.status)
        assertEquals("com.example", diagnostics.processName)
        assertFalse(JankHunter.isStarted())
        assertFalse(JankHunter.isRuntimeEnabled())
    }

    @Test
    fun runtimeEnableWithoutInitReturnsFalse() {
        assertFalse(JankHunter.setRuntimeEnabled(true, "remote_config"))
        assertEquals("runtime_enable_missing_init", JankHunter.initDiagnostics().status)
    }

    @Test
    fun externalStorageCanReplaceBootstrapStorageWithoutRestartingRuntime() {
        val root = tempDir()
        val bootstrap = File(root, "bootstrap")
        val target = FileStorage(File(root, "external"))
        val context = FailingLogDirectoryContext(File(root, "files").apply { mkdirs() })
        JankHunter.init(
            context,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .logDirectory(bootstrap)
                .build(),
        )
        JankHunterTelemetry.counter("storage.valve.before", 1L)

        assertEquals(JankHunterStorageSwitchResult.SWITCHED, JankHunter.switchBinaryStorage(target))

        JankHunterTelemetry.counter("storage.valve.after", 1L)
        JankHunter.shutdown()
        assertTrue(bootstrap.jhlogs().isEmpty())
        assertTrue(target.directory.jhlogs().size >= 2)
    }

    @Test
    fun externalStorageCanBeBoundWhileCollectionIsDisabled() {
        val root = tempDir()
        val bootstrap = File(root, "bootstrap")
        val target = FileStorage(File(root, "external"))
        val context = FailingLogDirectoryContext(File(root, "files").apply { mkdirs() })
        JankHunter.init(
            context,
            JankHunterConfig.builder()
                .runtimeEnabled(false)
                .autoStartCollectors(false)
                .logDirectory(bootstrap)
                .build(),
        )

        assertEquals(JankHunterStorageSwitchResult.SWITCHED, JankHunter.switchBinaryStorage(target))
        assertTrue(JankHunter.setRuntimeEnabled(true, "storage_ready"))
        JankHunterTelemetry.counter("storage.valve.enabled", 1L)
        JankHunter.shutdown()

        assertTrue(bootstrap.jhlogs().isEmpty())
        assertTrue(target.directory.jhlogs().isNotEmpty())
    }

    @Test
    fun nullStorageRestoresBuiltInStorageAndConsolidatesExternalSegments() {
        val root = tempDir()
        val bootstrap = File(root, "bootstrap")
        val target = FileStorage(File(root, "external"))
        val context = FailingLogDirectoryContext(File(root, "files").apply { mkdirs() })
        JankHunter.init(
            context,
            JankHunterConfig.builder()
                .autoStartCollectors(false)
                .logDirectory(bootstrap)
                .build(),
        )

        assertEquals(JankHunterStorageSwitchResult.SWITCHED, JankHunter.switchBinaryStorage(target))
        JankHunterTelemetry.counter("storage.valve.external", 1L)
        assertEquals(JankHunterStorageSwitchResult.SWITCHED, JankHunter.switchBinaryStorage(null))
        JankHunterTelemetry.counter("storage.valve.internal", 1L)
        JankHunter.shutdown()

        assertTrue(target.directory.jhlogs().isEmpty())
        assertTrue(bootstrap.jhlogs().size >= 3)
    }

    @Test
    fun binaryStorageSwitchRequiresInitializedRuntimeConfiguration() {
        assertEquals(JankHunterStorageSwitchResult.NOT_STARTED, JankHunter.switchBinaryStorage(FileStorage(tempDir())))
    }

    private fun tempDir(): File = Files.createTempDirectory("jankhunter-init-diagnostics-test").toFile()

    private fun awaitWriterFailure(): JankHunterInitDiagnostics {
        val deadlineNanos = System.nanoTime() + 5_000_000_000L
        while (System.nanoTime() < deadlineNanos) {
            val diagnostics = JankHunter.initDiagnostics()
            if (!JankHunter.isStarted() && diagnostics.failureClass != null) return diagnostics
            Thread.sleep(10L)
        }
        return JankHunter.initDiagnostics()
    }

    private class FailingLogDirectoryContext(
        private val filesDir: File,
    ) : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this

        override fun getPackageName(): String = "com.example"

        override fun getFilesDir(): File = filesDir

        override fun getSystemService(name: String): Any? = null
    }

    private class FileStorage(
        val directory: File,
    ) : JankHunterBinaryStorage {
        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            directory.mkdirs()
            return FileWriter(File(directory, fileName))
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
            directory.mkdirs()
            val file = File(directory, fileName)
            return object : JankHunterBinaryArtifact {
                override val path: String = file.absolutePath

                override fun commit() = Unit

                override fun abort() {
                    file.delete()
                }
            }
        }

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = directory.listFiles().orEmpty().map(File::getAbsolutePath)
    }

    private class FileWriter(file: File) : JankHunterBinaryWriter {
        private val output = FileOutputStream(file, true)
        private var bytes = file.length()
        override val path: String = file.absolutePath

        override fun bytesWritten(): Long = bytes

        override fun writeByte(byte: Byte) {
            output.write(byte.toInt())
            bytes++
        }

        override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) {
            output.write(bytes, offset, length)
            this.bytes += length
        }

        override fun flush() = output.flush()

        override fun close() = output.close()
    }

    private fun File.jhlogs(): List<File> = listFiles { file -> file.isFile && file.extension == "jhlog" }
        .orEmpty()
        .toList()
}
