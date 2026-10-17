package io.jankhunter.sample

import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterExecutionObservationMode
import io.jankhunter.runtime.JankHunterTelemetry
import io.jankhunter.runtime.JankHunterManifestConfig
import io.jankhunter.runtime.JankHunterStoragePolicy
import java.io.File
import java.util.UUID
import java.util.concurrent.Executor
import io.jankhunter.sample.concurrency.ExecutionObservationProbe
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class ExecutionObservationModeArtTest {
    @Test
    fun oneRemovalPreservesAnotherPlacementWhileDisabled() {
        JankHunter.shutdown()
        assertEquals(1, ExecutionObservationProbe().trackedRemoval())
    }

    @Test
    fun processChoiceSurvivesDeferredChangeAndStorageWhileRuntimeCanRestart() {
        val expected = JankHunterExecutionObservationMode.valueOf(
            InstrumentationRegistry.getArguments().getString("observation_mode") ?: "WRAPPERS")
        val next = if (expected == JankHunterExecutionObservationMode.IN_PLACE)
            JankHunterExecutionObservationMode.WRAPPERS else JankHunterExecutionObservationMode.IN_PLACE
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val directory = File(context.cacheDir, "observation-mode-${UUID.randomUUID()}")
        val probe = ExecutionObservationProbe()
        val executor = Executor { it.run() }
        JankHunter.shutdown()
        try {
            JankHunter.init(context, JankHunterManifestConfig.read(context).toBuilder()
                .autoStartCollectors(false).logDirectory(directory)
                .taskObservationMode(expected).coroutineObservationMode(expected).build())
            assertTrue(JankHunter.initDiagnostics().toString(), JankHunter.isStarted())
            assertEquals(expected, JankHunter.executionObservationModes().tasks)
            assertEquals(1, probe.beforeDisable())
            val reconfigured = JankHunter.reconfigure { it.taskObservationMode(next).coroutineObservationMode(next) }
            assertTrue(JankHunter.initDiagnostics().toString(), reconfigured)
            assertEquals(next, JankHunter.requestedExecutionObservationModes().tasks)
            assertEquals(expected, JankHunter.executionObservationModes().tasks)
            assertTrue(JankHunter.setRuntimeEnabled(false))
            val policy = JankHunterStoragePolicy(directory, Long.MAX_VALUE, Long.MAX_VALUE,
                setOf("jhlog"), emptySet(), 8192, true)
            assertTrue(JankHunter.reconfigure { it.storagePolicy(policy) })
            assertFalse(JankHunter.isStarted())
            assertFalse(JankHunter.isRuntimeEnabled())
            assertEquals(next, JankHunter.requestedExecutionObservationModes().tasks)
            assertEquals(2, probe.whileDisabled())
            assertSame(executor, JankHunterTelemetry.wrapExecutor(executor, "observation-mode"))
            assertEquals("One handle removed another placement of the same callback while disabled", 1, probe.trackedRemoval())
            assertTrue(JankHunter.setRuntimeEnabled(true))
            assertTrue(JankHunter.isStarted())
            val prepared = JankHunterTelemetry.wrapExecutor(executor, "observation-mode")
            if (expected == JankHunterExecutionObservationMode.IN_PLACE) assertSame(executor, prepared)
            else assertNotSame(executor, prepared)
            assertEquals(3, probe.afterEnable())
            assertEquals("One handle removed another placement of the same callback", 1, probe.trackedRemoval())
            assertEquals(expected, JankHunter.executionObservationModes().tasks)
            val archive = File(context.getExternalFilesDir(null) ?: context.filesDir, "observation-mode-${expected.name}.zip")
            if (archive.exists()) check(archive.delete())
            assertTrue(JankHunter.captureLogArchive(archive) != null)
        } finally {
            JankHunter.shutdown()
        }
    }
}
