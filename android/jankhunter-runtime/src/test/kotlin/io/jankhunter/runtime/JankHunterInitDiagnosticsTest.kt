package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import java.io.File
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
        assertNull(JankHunter.lastInitFailure())
        assertFalse(JankHunter.isStarted())
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
        assertNotNull(JankHunter.lastInitFailure())
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
}
