package io.jankhunter.sample

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterManifestConfig
import io.jankhunter.runtime.JankHunterRuntimeFeature
import io.jankhunter.sample.concurrency.ConcurrencyRegistryFootprint
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

/** Produces an ART heap for the host causal registry retained-size acceptance test. */
@RunWith(AndroidJUnit4::class)
class ConcurrencyRegistryFootprintTest {
    @Test fun capturesSaturatedRegistryWithoutStartingApplicationThreads() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        val builder = JankHunterManifestConfig.read(context).toBuilder()
            .enabled(true).concurrencyMaxTrackedEntities(4096)
            .systemSamplerEnabled(false).runtimeCallGraphEnabled(false)
            .memorySampleIntervalMs(600_000)
        JankHunterRuntimeFeature.entries.forEach { builder.runtimeFeatureEnabled(it, false) }
        builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS, true)
            .runtimeFeatureEnabled(JankHunterRuntimeFeature.THREAD_SAMPLING, true)
        instrumentation.runOnMainSync {
            JankHunter.shutdown()
            JankHunter.init(context, builder.build())
        }
        try {
            val dump = ConcurrencyRegistryFootprint(context).capture()
            assertTrue(dump.length() > 0L)
        } finally {
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
    }
}
