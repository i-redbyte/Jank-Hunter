package io.jankhunter.runtime

import org.junit.Assert.assertArrayEquals
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
            .ownerBlockThresholdMs(234)
            .httpSlowThresholdMs(345)
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
            .uiWindowP95ThresholdMs(22)
            .maxQueueSize(99)
            .sessionLogSizeLimitEnabled(false)
            .maxSessionLogSizeMiB(8)
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
            .build()

        assertFalse(config.enabled())
        assertFalse(config.runtimeEnabled())
        assertFalse(config.autoStartCollectors())
        assertEquals(123, config.mainThreadStallThresholdMs())
        assertEquals(234, config.ownerBlockThresholdMs())
        assertEquals(345, config.httpSlowThresholdMs())
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
        assertEquals(22, config.uiWindowP95ThresholdMs())
        assertEquals(99, config.maxQueueSize())
        assertFalse(config.sessionLogSizeLimitEnabled())
        assertEquals(8, config.maxSessionLogSizeMiB())
        assertEquals(0L, config.sessionLogSizeLimitBytes())
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
    }

    @Test
    fun defaultsAreLongRunningAppFriendly() {
        val config = JankHunterConfig.builder().build()

        assertTrue(config.enabled())
        assertTrue(config.runtimeEnabled())
        assertTrue(config.autoStartCollectors())
        assertTrue(config.systemSamplerEnabled())
        assertEquals(700L, config.mainThreadStallThresholdMs())
        assertEquals(250L, config.ownerBlockThresholdMs())
        assertEquals(1_000L, config.httpSlowThresholdMs())
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
        assertTrue(config.jankStatsEnabled())
        assertEquals(32L, config.jankFrameThresholdMs())
        assertEquals(32L, config.uiWindowP95ThresholdMs())
        assertTrue(config.mainProcessOnly())
        assertTrue(config.allowedProcesses().isEmpty())
        assertEquals(2048, config.maxQueueSize())
        assertTrue(config.sessionLogSizeLimitEnabled())
        assertEquals(16, config.maxSessionLogSizeMiB())
        assertEquals(16L * 1024L * 1024L, config.sessionLogSizeLimitBytes())
        assertEquals(8192, config.maxDictionaryEntries())
        assertEquals(1024, config.maxDictionaryValueBytes())
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
    fun sessionLimitManifestKeysUseTheCanonicalApiNames() {
        assertEquals(
            "io.jankhunter.session_log_size_limit_enabled",
            JankHunterConfig.META_SESSION_LOG_SIZE_LIMIT_ENABLED,
        )
        assertEquals("io.jankhunter.max_session_log_size_mib", JankHunterConfig.META_MAX_SESSION_LOG_SIZE_MIB)
        assertEquals("io.jankhunter.symbol_namespace", JankHunterConfig.META_SYMBOL_NAMESPACE)

        val configMethods = JankHunterConfig::class.java.methods.mapTo(mutableSetOf()) { it.name }
        val builderMethods = JankHunterConfig.Builder::class.java.methods.mapTo(mutableSetOf()) { it.name }
        assertFalse("maxSessionLogBytes" in configMethods)
        assertFalse("maxSessionLogsBytes" in configMethods)
        assertFalse("maxSessionLogBytes" in builderMethods)
        assertFalse("maxSessionLogsBytes" in builderMethods)
    }

    @Test
    fun sessionLogMiBConversionDoesNotOverflow() {
        val config = JankHunterConfig.builder()
            .maxSessionLogSizeMiB(Int.MAX_VALUE)
            .build()

        assertEquals(
            Int.MAX_VALUE.toLong() * 1024L * 1024L,
            config.sessionLogSizeLimitBytes(),
        )
    }

    @Test
    fun symbolNamespaceIsStrictlyDecodedAndDefensivelyCopied() {
        val expected = byteArrayOf(
            0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
            0x88.toByte(), 0x99.toByte(), 0xaa.toByte(), 0xbb.toByte(),
            0xcc.toByte(), 0xdd.toByte(), 0xee.toByte(), 0xff.toByte(),
        )
        val source = JankHunterConfig.decodeSymbolNamespace("00112233445566778899aabbccddeeff")
        val config = JankHunterConfig.builder().symbolNamespace(source).build()
        source.fill(0)

        assertArrayEquals(expected, config.symbolNamespace())
        val exposed = config.symbolNamespace()
        exposed.fill(0)
        assertArrayEquals(expected, config.toBuilder().build().symbolNamespace())
        assertArrayEquals(ByteArray(0), JankHunterConfig.decodeSymbolNamespace("abc"))
        assertArrayEquals(ByteArray(0), JankHunterConfig.decodeSymbolNamespace("00112233445566778899aabbccddeeGG"))
        assertArrayEquals(ByteArray(0), JankHunterConfig.decodeSymbolNamespace("00112233445566778899AABBCCDDEEFF"))
        assertArrayEquals(ByteArray(0), JankHunterConfig.decodeSymbolNamespace("0011aaff"))
        assertArrayEquals(ByteArray(0), JankHunterConfig.decodeSymbolNamespace(" 00112233445566778899aabbccddeeff"))
    }

    @Test
    fun buildNamespaceOverridesOpaqueNamespaceWithoutChangingManualRuntimePolicy() {
        val forged = ByteArray(16) { 0x7f }
        val buildNamespace = ByteArray(16) { index -> index.toByte() }
        val manual = JankHunterConfig.builder()
            .runtimeEnabled(false)
            .mainThreadStallThresholdMs(321L)
            .symbolNamespace(forged)
            .build()

        val effective = JankHunterConfig.withBuildSymbolNamespace(manual, buildNamespace)
        buildNamespace.fill(0)

        assertFalse(effective.runtimeEnabled())
        assertEquals(321L, effective.mainThreadStallThresholdMs())
        assertArrayEquals(ByteArray(16) { index -> index.toByte() }, effective.symbolNamespace())
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
            .retainedHeapDumpPrivacyApproved(false)
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
            .ownerBlockThresholdMs(-2)
            .httpSlowThresholdMs(-3)
            .memorySampleIntervalMs(0)
            .systemSampleIntervalMs(-5)
            .retainedObjectDelayMs(-10)
            .retainedHeapDumpMinIntervalMs(-20)
            .retainedHeapDumpMaxCount(-1)
            .fpsWindowMs(0)
            .jankFrameThresholdMs(-1)
            .uiWindowP95ThresholdMs(-2)
            .maxSessionLogSizeMiB(0)
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
        assertEquals(1, config.ownerBlockThresholdMs())
        assertEquals(1, config.httpSlowThresholdMs())
        assertEquals(1, config.memorySampleIntervalMs())
        assertEquals(1, config.systemSampleIntervalMs())
        assertEquals(0, config.retainedObjectDelayMs())
        assertEquals(0, config.retainedHeapDumpMinIntervalMs())
        assertEquals(0, config.retainedHeapDumpMaxCount())
        assertEquals(1, config.fpsWindowMs())
        assertEquals(1, config.jankFrameThresholdMs())
        assertEquals(1, config.uiWindowP95ThresholdMs())
        assertEquals(1, config.maxSessionLogSizeMiB())
        assertEquals(1024L * 1024L, config.sessionLogSizeLimitBytes())
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
