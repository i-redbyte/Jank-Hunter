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
            .mainProcessOnly(true)
            .allowedProcesses(listOf("com.example", "com.example:remote"))
            .processNameRedactor { "redacted.$it" }
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
        assertTrue(config.mainProcessOnly())
        assertEquals(setOf("com.example", "com.example:remote"), config.allowedProcesses())
        assertEquals("redacted.com.example", config.redactProcessName("com.example"))
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
        assertFalse(config.mainProcessOnly())
        assertTrue(config.allowedProcesses().isEmpty())
        assertEquals(2048, config.maxQueueSize())
    }

    @Test
    fun processPolicyHonorsMainProcessAndAllowList() {
        val mainOnly = JankHunterConfig.builder()
            .mainProcessOnly(true)
            .build()

        assertTrue(mainOnly.isProcessAllowed("com.example", "com.example"))
        assertFalse(mainOnly.isProcessAllowed("com.example:remote", "com.example"))

        val allowList = JankHunterConfig.builder()
            .allowedProcesses(listOf("com.example:sync"))
            .build()

        assertFalse(allowList.isProcessAllowed("com.example", "com.example"))
        assertTrue(allowList.isProcessAllowed("com.example:sync", "com.example"))
    }

    @Test
    fun defaultRedactorRemovesCommonPathIdentifiers() {
        val config = JankHunterConfig.builder().build()

        assertEquals(
            "GET /users/{id}/orders/{uuid}/email/{email}",
            config.redactRoute("GET /users/123/orders/550e8400-e29b-41d4-a716-446655440000/email/a@b.com"),
        )
    }
}
