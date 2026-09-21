package io.jankhunter.gradle

import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ArtTiDynamicScalerTest {
    @Test
    fun scalesCausalPresetForLargeFootprintAndStorageBudget() {
        val base = EffectiveArtTiConfigResolver.resolve(artTi { mode.set(ArtTiMode.CAUSAL) })
        val footprint = ArtTiInstrumentationFootprint(
            instrumentedClassCount = 4_000,
            instrumentedMethodCount = 32_000,
            hookCount = 12_000,
            instrumentationPassCount = 2,
            gradleModuleCount = 8,
        )
        val scaled = ArtTiDynamicScaler.scale(
            base = base,
            footprint = footprint,
            storageLimitMiB = 350,
            scaleToApplicationSize = true,
            overrides = ArtTiExplicitOverrides(),
        )
        assertTrue(scaled.transportCapacity > base.transportCapacity)
        assertTrue(scaled.transportCapacity and (scaled.transportCapacity - 1) == 0)
        assertTrue(scaled.maxMethodDefinitions > base.maxMethodDefinitions)
        assertTrue(scaled.configHash != base.configHash)
    }

    @Test
    fun respectsExplicitTransportOverride() {
        val dsl = artTi {
            mode.set(ArtTiMode.CAUSAL)
            transport.capacity.set(2048)
        }
        val base = EffectiveArtTiConfigResolver.resolve(dsl)
        val scaled = ArtTiDynamicScaler.scale(
            base = base,
            footprint = ArtTiInstrumentationFootprint(10_000, 10_000, 10_000, 2, 4),
            storageLimitMiB = 256,
            scaleToApplicationSize = true,
            overrides = EffectiveArtTiConfigResolver.explicitOverrides(dsl),
        )
        assertEquals(2048, scaled.transportCapacity)
    }

    @Test
    fun customModeIsNotScaled() {
        val dsl = artTi {
            mode.set(ArtTiMode.CUSTOM)
            garbageCollection.enabled.set(true)
            threads.lifecycle.set(true)
            threads.maxTrackedThreads.set(512)
            monitorContention.enabled.set(true)
            monitorContention.minDurationMs.set(8)
            monitorContention.maxOpenIntervals.set(1024)
            stackSampling.enabled.set(true)
            stackSampling.maxDepth.set(64)
            stackSampling.onMainThreadStall.set(true)
            stackSampling.onLongContention.set(true)
            stackSampling.minTriggerIntervalMs.set(250)
            stackSampling.maxSamplesPerMinute.set(120)
            stackSampling.maxStackDefinitions.set(1024)
            stackSampling.maxMethodDefinitions.set(4096)
            transport.capacity.set(4096)
            transport.drainBatchSize.set(256)
            transport.overflowPolicy.set(ArtTiOverflowPolicy.DROP_AND_COUNT)
        }
        val base = EffectiveArtTiConfigResolver.resolve(dsl)
        val scaled = ArtTiDynamicScaler.scale(
            base = base,
            footprint = ArtTiInstrumentationFootprint(10_000, 10_000, 10_000, 2, 4),
            storageLimitMiB = 256,
            scaleToApplicationSize = true,
            overrides = ArtTiExplicitOverrides(),
        )
        assertEquals(base, scaled)
    }

    private fun artTi(configure: JankHunterExtension.ArtTi.() -> Unit): JankHunterExtension.ArtTi {
        return ProjectBuilder.builder().build().objects
            .newInstance(JankHunterExtension.ArtTi::class.java)
            .apply(configure)
    }
}
