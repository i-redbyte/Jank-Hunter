package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeSemanticTelemetryTest {
    @Test
    fun effectiveFeatureGateIsTheSoleAdmissionReadForSemanticEnter() {
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        val config = JankHunterConfig.builder()
            .composeTracingEnabled(true)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.COMPOSE, true)
            .build()

        graph.state.featureGate.activate(config)

        assertTrue(graph.semanticTelemetry.enter(JankHunterSemanticWork.COMPOSE_DRAW) > 0L)

        graph.state.featureGate.deactivate()
        assertEquals(0L, graph.semanticTelemetry.enter(JankHunterSemanticWork.COMPOSE_DRAW))
    }
}
