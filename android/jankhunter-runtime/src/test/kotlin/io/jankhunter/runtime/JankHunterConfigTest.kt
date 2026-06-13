package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterConfigTest {
    @Test
    fun builderKeepsExplicitRuntimePolicy() {
        val config = JankHunterConfig.builder()
            .enabled(false)
            .autoStartCollectors(false)
            .mainThreadStallThresholdMs(123)
            .memorySampleIntervalMs(456)
            .systemSamplerEnabled(false)
            .systemSampleIntervalMs(654)
            .processExitInfoEnabled(false)
            .objectWatcherEnabled(false)
            .retainedObjectDelayMs(321)
            .fpsMonitorEnabled(false)
            .fpsWindowMs(789)
            .jankFrameThresholdMs(11)
            .maxQueueSize(99)
            .maxLogBytes(1024)
            .flushIntervalMs(12)
            .build()

        assertFalse(config.enabled())
        assertFalse(config.autoStartCollectors())
        assertEquals(123, config.mainThreadStallThresholdMs())
        assertEquals(456, config.memorySampleIntervalMs())
        assertFalse(config.systemSamplerEnabled())
        assertEquals(654, config.systemSampleIntervalMs())
        assertFalse(config.processExitInfoEnabled())
        assertFalse(config.objectWatcherEnabled())
        assertEquals(321, config.retainedObjectDelayMs())
        assertFalse(config.fpsMonitorEnabled())
        assertEquals(789, config.fpsWindowMs())
        assertEquals(11, config.jankFrameThresholdMs())
        assertEquals(99, config.maxQueueSize())
        assertEquals(1024, config.maxLogBytes())
        assertEquals(12, config.flushIntervalMs())
    }

    @Test
    fun defaultsAreDebugCollectorFriendly() {
        val config = JankHunterConfig.builder().build()

        assertTrue(config.enabled())
        assertTrue(config.autoStartCollectors())
        assertTrue(config.systemSamplerEnabled())
        assertTrue(config.processExitInfoEnabled())
        assertTrue(config.objectWatcherEnabled())
        assertTrue(config.fpsMonitorEnabled())
        assertEquals(2048, config.maxQueueSize())
    }
}
