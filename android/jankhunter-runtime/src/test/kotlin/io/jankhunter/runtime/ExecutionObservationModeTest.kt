package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExecutionObservationModeTest {
    @Test
    fun defaultAndBuilderRoundTripPreserveIndependentModes() {
        val baseline = JankHunterConfig.builder().build()
        assertEquals(JankHunterExecutionObservationMode.WRAPPERS, baseline.taskObservationMode())
        assertEquals(JankHunterExecutionObservationMode.WRAPPERS, baseline.coroutineObservationMode())
        val config = baseline.toBuilder().taskObservationMode(JankHunterExecutionObservationMode.IN_PLACE).build()
        assertEquals(JankHunterExecutionObservationMode.IN_PLACE, config.toBuilder().build().taskObservationMode())
        assertEquals(JankHunterExecutionObservationMode.WRAPPERS, config.coroutineObservationMode())
    }

    @Test
    fun activeModesSurviveReconfigureDisableAndShutdownUntilProcessExit() {
        val state = RuntimeState()
        val initial = JankHunterConfig.builder()
            .taskObservationMode(JankHunterExecutionObservationMode.IN_PLACE).build()
        state.bindInitialConfiguration(initial)
        state.bindRuntimeEnabled(false)
        state.bindCurrentConfiguration(checkNotNull(state.config).toBuilder()
            .taskObservationMode(JankHunterExecutionObservationMode.WRAPPERS)
            .coroutineObservationMode(JankHunterExecutionObservationMode.IN_PLACE).build())
        val active = state.observationModes()
        assertEquals(JankHunterExecutionObservationMode.IN_PLACE, active.tasks)
        assertEquals(JankHunterExecutionObservationMode.WRAPPERS, active.coroutines)
        assertFalse(state.configurationSnapshot().desiredRuntimeEnabled)
        state.clearConfiguration()
        state.bindInitialConfiguration(JankHunterConfig.builder().build())
        assertEquals(active, state.observationModes())
        assertTrue(state.configurationSnapshot().desiredRuntimeEnabled)
    }

    @Test
    fun readingDefaultBeforeInitializationDoesNotFixProcessChoice() {
        val state = RuntimeState()
        assertEquals(JankHunterExecutionObservationMode.WRAPPERS, state.observationModes().tasks)
        state.bindInitialConfiguration(JankHunterConfig.builder()
            .taskObservationMode(JankHunterExecutionObservationMode.IN_PLACE).build())
        assertEquals(JankHunterExecutionObservationMode.IN_PLACE, state.observationModes().tasks)
    }

    @Test
    fun inPlaceProducerPolicyCannotWaitEvenWhenRequestedConfigurationAllowsIt() {
        val config = JankHunterConfig.builder().mainThreadAdmissionWaitMs(50L).backgroundAdmissionWaitMs(100L).build()
        val modes = JankHunterExecutionObservationModes(tasks = JankHunterExecutionObservationMode.IN_PLACE)
        val producer = config.forObservationModes(modes)
        assertEquals(0L, producer.mainThreadAdmissionWaitMs())
        assertEquals(0L, producer.backgroundAdmissionWaitMs())
        assertEquals(100L, config.backgroundAdmissionWaitMs())
        assertEquals(100L, config.forObservationModes(JankHunterExecutionObservationModes()).backgroundAdmissionWaitMs())
    }
}
