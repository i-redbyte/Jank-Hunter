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
    fun observerReceivesInitAndShutdownEvents() {
        val events = mutableListOf<RuntimeEvent>()
        val observer = RuntimeObserver { event -> events += event }
        val subscription = JankHunter.addRuntimeObserver(observer)
        try {
            JankHunter.init(null)
            JankHunter.shutdown()
        } finally {
            subscription.close()
        }

        assertTrue(events.any { it is RuntimeEvent.InitStatus && it.diagnostics.status == "missing_context" })
        assertTrue(events.any { it is RuntimeEvent.ShutdownStarted })
        assertTrue(events.any { it is RuntimeEvent.ShutdownFinished })
    }

    @Test
    fun observerSubscriptionStopsEventsAfterClose() {
        val events = mutableListOf<RuntimeEvent>()
        val subscription = JankHunter.addRuntimeObserver { event -> events += event }

        subscription.close()
        JankHunter.init(null)

        assertTrue(events.isEmpty())
    }

    @Test
    fun initRecordsStartupFailureWithoutThrowing() {
        val filesDir = File(tempDir(), "files").apply {
            writeText("not a directory")
        }
        val context = FailingLogDirectoryContext(filesDir)

        JankHunter.init(context, JankHunterConfig.builder().autoStartCollectors(false).build())

        val diagnostics = JankHunter.initDiagnostics()
        assertEquals("failed", diagnostics.status)
        assertEquals("IllegalStateException", diagnostics.failureClass)
        assertTrue(diagnostics.failureMessage.orEmpty().contains("Cannot create Jank Hunter log directory"))
        assertEquals("com.example", diagnostics.processName)
        assertTrue(diagnostics.logDirectory.orEmpty().endsWith("files/jankhunter"))
        assertNotNull(JankHunter.lastInitFailure())
        assertFalse(JankHunter.isStarted())
    }

    private fun tempDir(): File = Files.createTempDirectory("jankhunter-init-diagnostics-test").toFile()

    private class FailingLogDirectoryContext(
        private val filesDir: File,
    ) : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this

        override fun getPackageName(): String = "com.example"

        override fun getFilesDir(): File = filesDir

        override fun getSystemService(name: String): Any? = null
    }
}
