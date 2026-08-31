package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class MemoryPressureSamplerTest {
    @Test
    fun gaugeRecorderUsesPrimitivePort() {
        val field = MemoryPressureSampler::class.java.getDeclaredField("recordGauge")

        assertFalse(field.type == Function2::class.java)
    }

    @Test
    fun recordsCurrentProcessTrimLevel() {
        val gauges = mutableListOf<Pair<String, Long>>()
        val sampler = MemoryPressureSampler(
            readTrimLevel = { 40 },
            recordGauge = { name, value -> gauges += name to value },
        )

        sampler.sample()

        assertEquals(listOf("memory.trim.last_level" to 40L), gauges)
    }

    @Test
    fun ignoresUnavailableProcessState() {
        val gauges = mutableListOf<Pair<String, Long>>()
        val sampler = MemoryPressureSampler(
            readTrimLevel = { error("unavailable") },
            recordGauge = { name, value -> gauges += name to value },
        )

        sampler.sample()

        assertEquals(emptyList<Pair<String, Long>>(), gauges)
    }
}
