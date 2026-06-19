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
            .runtimeEnabled(false)
            .autoStartCollectors(false)
            .mainThreadStallThresholdMs(123)
            .memorySampleIntervalMs(456)
            .systemSamplerEnabled(false)
            .systemSampleIntervalMs(654)
            .mainLooperDispatchMonitorEnabled(true)
            .processExitInfoEnabled(false)
            .objectWatcherEnabled(false)
            .retainedObjectDelayMs(321)
            .retainedObjectForceGcEnabled(true)
            .retainedHeapDumpEnabled(true)
            .retainedHeapDumpPrivacyApproved(true)
            .retainedHeapDumpMinIntervalMs(987)
            .retainedHeapDumpMaxCount(3)
            .retainedHeapDumpMinRetainedAgeMs(654)
            .fpsMonitorEnabled(false)
            .jankStatsEnabled(true)
            .fpsWindowMs(789)
            .jankFrameThresholdMs(11)
            .maxQueueSize(99)
            .maxLogBytes(1024)
            .maxLogDirectoryBytes(4096)
            .logCompressionEnabled(false)
            .logBucket(JankHunterLogBucket.SESSION)
            .maxDictionaryEntries(1234)
            .maxDictionaryValueBytes(64)
            .flushIntervalMs(12)
            .adaptiveSamplingEnabled(false)
            .adaptiveMemoryStableIntervalMs(13)
            .adaptiveContextStableIntervalMs(14)
            .metricAggregationEnabled(false)
            .metricAggregationWindowMs(15)
            .maxMetricAggregationKeys(16)
            .maxLogSpamKeys(17)
            .maxRuntimeCallGraphKeys(18)
            .maxHandlerTrackingEntries(19)
            .maxHandlerWrappersPerRunnable(20)
            .mainProcessOnly(true)
            .allowedProcesses(listOf("com.example", "com.example:remote"))
            .processNameRedactor { "redacted.$it" }
            .deviceInfoEnabled(false)
            .build()

        assertFalse(config.enabled())
        assertFalse(config.runtimeEnabled())
        assertFalse(config.autoStartCollectors())
        assertEquals(123, config.mainThreadStallThresholdMs())
        assertEquals(456, config.memorySampleIntervalMs())
        assertFalse(config.systemSamplerEnabled())
        assertEquals(654, config.systemSampleIntervalMs())
        assertTrue(config.mainLooperDispatchMonitorEnabled())
        assertFalse(config.processExitInfoEnabled())
        assertFalse(config.objectWatcherEnabled())
        assertEquals(321, config.retainedObjectDelayMs())
        assertTrue(config.retainedObjectForceGcEnabled())
        assertTrue(config.retainedHeapDumpEnabled())
        assertTrue(config.retainedHeapDumpPrivacyApproved())
        assertEquals(987, config.retainedHeapDumpMinIntervalMs())
        assertEquals(3, config.retainedHeapDumpMaxCount())
        assertEquals(654, config.retainedHeapDumpMinRetainedAgeMs())
        assertFalse(config.fpsMonitorEnabled())
        assertTrue(config.jankStatsEnabled())
        assertEquals(789, config.fpsWindowMs())
        assertEquals(11, config.jankFrameThresholdMs())
        assertEquals(99, config.maxQueueSize())
        assertEquals(1024, config.maxLogBytes())
        assertEquals(4096, config.maxLogDirectoryBytes())
        assertFalse(config.logCompressionEnabled())
        assertEquals(JankHunterLogBucket.SESSION, config.logBucket())
        assertEquals(1234, config.maxDictionaryEntries())
        assertEquals(64, config.maxDictionaryValueBytes())
        assertEquals(12, config.flushIntervalMs())
        assertFalse(config.adaptiveSamplingEnabled())
        assertEquals(13, config.adaptiveMemoryStableIntervalMs())
        assertEquals(14, config.adaptiveContextStableIntervalMs())
        assertFalse(config.metricAggregationEnabled())
        assertEquals(15, config.metricAggregationWindowMs())
        assertEquals(16, config.maxMetricAggregationKeys())
        assertEquals(17, config.maxLogSpamKeys())
        assertEquals(18, config.maxRuntimeCallGraphKeys())
        assertEquals(19, config.maxHandlerTrackingEntries())
        assertEquals(20, config.maxHandlerWrappersPerRunnable())
        assertTrue(config.mainProcessOnly())
        assertEquals(setOf("com.example", "com.example:remote"), config.allowedProcesses())
        assertEquals("redacted.com.example", config.redactProcessName("com.example"))
        assertFalse(config.deviceInfoEnabled())
    }

    @Test
    fun defaultsAreDebugCollectorFriendly() {
        val config = JankHunterConfig.builder().build()

        assertTrue(config.enabled())
        assertTrue(config.runtimeEnabled())
        assertTrue(config.autoStartCollectors())
        assertTrue(config.systemSamplerEnabled())
        assertFalse(config.mainLooperDispatchMonitorEnabled())
        assertTrue(config.processExitInfoEnabled())
        assertTrue(config.objectWatcherEnabled())
        assertFalse(config.retainedObjectForceGcEnabled())
        assertFalse(config.retainedHeapDumpEnabled())
        assertFalse(config.retainedHeapDumpPrivacyApproved())
        assertEquals(10 * 60_000L, config.retainedHeapDumpMinIntervalMs())
        assertEquals(1, config.retainedHeapDumpMaxCount())
        assertEquals(30_000L, config.retainedHeapDumpMinRetainedAgeMs())
        assertTrue(config.fpsMonitorEnabled())
        assertFalse(config.jankStatsEnabled())
        assertTrue(config.mainProcessOnly())
        assertTrue(config.allowedProcesses().isEmpty())
        assertTrue(config.deviceInfoEnabled())
        assertEquals(2048, config.maxQueueSize())
        assertEquals(5L * 1024L * 1024L, config.maxLogBytes())
        assertEquals(25L * 1024L * 1024L, config.maxLogDirectoryBytes())
        assertTrue(config.logCompressionEnabled())
        assertEquals(JankHunterLogBucket.DAILY, config.logBucket())
        assertEquals(8192, config.maxDictionaryEntries())
        assertEquals(256, config.maxDictionaryValueBytes())
        assertTrue(config.adaptiveSamplingEnabled())
        assertEquals(60_000L, config.adaptiveMemoryStableIntervalMs())
        assertEquals(60_000L, config.adaptiveContextStableIntervalMs())
        assertTrue(config.metricAggregationEnabled())
        assertEquals(5_000L, config.metricAggregationWindowMs())
        assertEquals(2048, config.maxMetricAggregationKeys())
        assertEquals(2048, config.maxLogSpamKeys())
        assertEquals(4096, config.maxRuntimeCallGraphKeys())
        assertEquals(4096, config.maxHandlerTrackingEntries())
        assertEquals(32, config.maxHandlerWrappersPerRunnable())
    }

    @Test
    fun queueSizeIsClampedToKeepWriterConstructible() {
        val zero = JankHunterConfig.builder()
            .maxQueueSize(0)
            .build()
        val negative = JankHunterConfig.builder()
            .maxQueueSize(-10)
            .build()

        assertEquals(1, zero.maxQueueSize())
        assertEquals(1, negative.maxQueueSize())
    }

    @Test
    fun retainedHeapDumpRequiresExplicitPrivacyApproval() {
        val unapproved = JankHunterConfig.builder()
            .retainedHeapDumpEnabled(true)
            .build()
        val approved = JankHunterConfig.builder()
            .retainedHeapDumpEnabled(true)
            .retainedHeapDumpPrivacyApproved(true)
            .build()

        assertFalse(unapproved.retainedHeapDumpEnabled())
        assertTrue(approved.retainedHeapDumpEnabled())
    }

    @Test
    fun invalidNumericConfigIsClampedToSafeRuntimeValues() {
        val config = JankHunterConfig.builder()
            .mainThreadStallThresholdMs(-1)
            .memorySampleIntervalMs(0)
            .systemSampleIntervalMs(-5)
            .retainedObjectDelayMs(-10)
            .retainedHeapDumpMinIntervalMs(-20)
            .retainedHeapDumpMaxCount(-1)
            .fpsWindowMs(0)
            .jankFrameThresholdMs(-1)
            .maxLogBytes(0)
            .maxLogDirectoryBytes(-1)
            .maxDictionaryEntries(-1)
            .maxDictionaryValueBytes(0)
            .flushIntervalMs(0)
            .adaptiveMemoryStableIntervalMs(-1)
            .adaptiveContextStableIntervalMs(-1)
            .metricAggregationWindowMs(0)
            .maxMetricAggregationKeys(-1)
            .maxLogSpamKeys(-1)
            .maxRuntimeCallGraphKeys(-1)
            .maxHandlerTrackingEntries(-1)
            .maxHandlerWrappersPerRunnable(-1)
            .build()

        assertEquals(1, config.mainThreadStallThresholdMs())
        assertEquals(1, config.memorySampleIntervalMs())
        assertEquals(1, config.systemSampleIntervalMs())
        assertEquals(0, config.retainedObjectDelayMs())
        assertEquals(0, config.retainedHeapDumpMinIntervalMs())
        assertEquals(0, config.retainedHeapDumpMaxCount())
        assertEquals(1, config.fpsWindowMs())
        assertEquals(1, config.jankFrameThresholdMs())
        assertEquals(1, config.maxLogBytes())
        assertEquals(1, config.maxLogDirectoryBytes())
        assertEquals(0, config.maxDictionaryEntries())
        assertEquals(1, config.maxDictionaryValueBytes())
        assertEquals(1, config.flushIntervalMs())
        assertEquals(0, config.adaptiveMemoryStableIntervalMs())
        assertEquals(0, config.adaptiveContextStableIntervalMs())
        assertEquals(1, config.metricAggregationWindowMs())
        assertEquals(0, config.maxMetricAggregationKeys())
        assertEquals(0, config.maxLogSpamKeys())
        assertEquals(0, config.maxRuntimeCallGraphKeys())
        assertEquals(0, config.maxHandlerTrackingEntries())
        assertEquals(0, config.maxHandlerWrappersPerRunnable())
    }

    @Test
    fun manifestMetadataAcceptsAndroidXmlValueTypes() {
        assertEquals(600_000L, JankHunterConfig.coerceMetadataLong("600000", 1L))
        assertEquals(123L, JankHunterConfig.coerceMetadataLong(123, 1L))
        assertEquals(456L, JankHunterConfig.coerceMetadataLong(456L, 1L))
        assertEquals(42, JankHunterConfig.coerceMetadataInt("42", 1))
        assertEquals(7, JankHunterConfig.coerceMetadataInt(7L, 1))
        assertTrue(JankHunterConfig.coerceMetadataBoolean("true", false))
        assertFalse(JankHunterConfig.coerceMetadataBoolean("0", true))
        assertTrue(JankHunterConfig.coerceMetadataBoolean(1, false))
        assertFalse(JankHunterConfig.coerceMetadataBoolean(false, true))
        assertEquals(9L, JankHunterConfig.coerceMetadataLong("not-a-number", 9L))
        assertEquals(9, JankHunterConfig.coerceMetadataInt("not-a-number", 9))
        assertTrue(JankHunterConfig.coerceMetadataBoolean("maybe", true))
        assertEquals(
            JankHunterLogBucket.SESSION,
            JankHunterConfig.coerceMetadataLogBucket("session", JankHunterLogBucket.DAILY),
        )
        assertEquals(
            JankHunterLogBucket.DAILY,
            JankHunterConfig.coerceMetadataLogBucket("DAILY", JankHunterLogBucket.SESSION),
        )
        assertEquals(
            JankHunterLogBucket.SESSION,
            JankHunterConfig.coerceMetadataLogBucket("unknown", JankHunterLogBucket.SESSION),
        )
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
