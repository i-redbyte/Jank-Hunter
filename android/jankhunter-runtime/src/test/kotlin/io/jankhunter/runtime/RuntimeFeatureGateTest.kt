package io.jankhunter.runtime

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeFeatureGateTest {
    @Test
    fun gatePublishesOnlyEffectiveConfiguredFeatures() {
        val gate = RuntimeFeatureGate()
        val config = JankHunterConfig.builder()
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.RUNTIME_IO, true)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.BYTECODE_IO, true)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.COMPOSE, true)
            .ioTracingEnabled(false)
            .composeTracingEnabled(true)
            .build()

        assertFalse(gate.isEnabled(JankHunterRuntimeFeature.COMPOSE))

        gate.activate(config)

        assertTrue(gate.isEnabled(JankHunterRuntimeFeature.COMPOSE))
        assertFalse(gate.isEnabled(JankHunterRuntimeFeature.RUNTIME_IO))
        assertFalse(gate.isEnabled(JankHunterRuntimeFeature.BYTECODE_IO))

        gate.deactivate()
        assertFalse(gate.isEnabled(JankHunterRuntimeFeature.COMPOSE))
    }

    @Test
    fun coordinatorClosesGateBeforeStopTransition() {
        val state = RuntimeState()
        val coordinator = RuntimeCoordinator(state) { 1L }
        val config = JankHunterConfig.builder().build()

        assertTrue(coordinator.tryBeginStart())
        coordinator.markStarted(config)
        assertTrue(state.featureGate.isEnabled(JankHunterRuntimeFeature.HANDLERS))

        assertTrue(coordinator.beginStop())
        assertFalse(state.featureGate.isEnabled(JankHunterRuntimeFeature.HANDLERS))
    }
}
