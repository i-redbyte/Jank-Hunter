package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.AsyncWriterProducer
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LegacyLifecycleCoverageTest {
    @Test fun legacyZeroTargetCallsStillDeclarePartialCoverage() = checkCoverage(Any(), typed = false, expected = 2)
    @Test fun typedFallbackWithoutGeneratedAccessorIsAlsoPartial() = checkCoverage(Any(), typed = true, expected = 2)
    @Test fun upgradedOldCallWithGeneratedAccessorIsNotLegacyFallback() = checkCoverage(Accessor(), typed = false, expected = 0)
    @Test fun generatedTypedCallIsNotLegacyFallback() = checkCoverage(Accessor(), typed = true, expected = 0)
    @Test fun nullIsNotAnObservedLegacyCall() = checkCoverage(null, typed = false, expected = 0)

    @Test fun bothHookDescriptorsRemainLinkable() {
        JankHunterHooks::class.java.getMethod("watchLifecycleObject", Any::class.java, String::class.java, String::class.java)
        JankHunterHooks::class.java.getMethod("watchLifecycleObject", Any::class.java, Int::class.javaPrimitiveType, String::class.java, String::class.java)
    }

    private fun checkCoverage(instance: Any?, typed: Boolean, expected: Long) {
        val directory = Files.createTempDirectory("jh-legacy-coverage").toFile()
        val config = JankHunterConfig.builder().metricAggregationEnabled(true).maxMetricAggregationKeys(1).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val watcher = ObjectRetentionWatcher(1000)
        val state = RuntimeState().apply { objectRetentionWatcher = watcher }
        val access = RuntimeTelemetryAccess(
            state,
            ContextTracker(),
            RuntimeCoordinator(state) { 0L },
            { 0L },
            { 100 },
            OptionalIntegrationRegistry.disabledForTests(),
        )
        var contextUpdates = 0L
        val metrics = RuntimeMetricsService(1, { 0L }, { writer }, { config }, { contextUpdates++ }, { false }, { _, _ -> false })
        val telemetry = RuntimeRetentionTelemetry(state, access, metrics) { 0L }
        try {
            repeat(2) {
                if (typed) telemetry.watchLifecycleObject(instance, 0, "onDestroyView", "owner")
                else telemetry.watchLifecycleObject(instance, "onDestroyView", "owner")
            }
            assertEquals(expected, contextUpdates)
            val producer = AsyncLogWriter::class.java.getDeclaredField("producer").apply { isAccessible = true }.get(writer) as AsyncWriterProducer
            assertEquals(expected, producer.acceptedSequence)
            assertTrue(writer.flushBlocking())
        } finally {
            watcher.stop()
            assertTrue(writer.close())
            directory.deleteRecursively()
        }
    }

    private class Accessor : JankHunterLifecycleAccessorV1 {
        override fun jankHunterLifecycleKindV1() = 2
        override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) = Unit
    }
}
