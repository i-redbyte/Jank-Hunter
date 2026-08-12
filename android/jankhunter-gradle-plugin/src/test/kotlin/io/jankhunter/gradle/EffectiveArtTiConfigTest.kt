package io.jankhunter.gradle

import org.gradle.api.GradleException
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class EffectiveArtTiConfigTest {
    @Test
    fun defaultsOffAndCausalPresetIsBoundedAndDeterministic() {
        val dsl = artTi()

        assertFalse(EffectiveArtTiConfigResolver.resolve(dsl).enabled)

        dsl.mode.set(ArtTiMode.CAUSAL)
        dsl.stackSampling.maxDepth.set(96)
        val first = EffectiveArtTiConfigResolver.resolve(dsl)
        val second = EffectiveArtTiConfigResolver.resolve(dsl)

        assertTrue(first.enabled)
        assertTrue(first.garbageCollectionEnabled)
        assertTrue(first.threadLifecycleEnabled)
        assertTrue(first.monitorContentionEnabled)
        assertTrue(first.stackSamplingEnabled)
        assertEquals(96, first.maxStackDepth)
        assertEquals(first, second)
        assertNotEquals(0L, first.configHash)
        assertTrue(first.nativeAgentOptions().contains("profile=2"))
        assertTrue(first.nativeAgentOptions().contains("cap=0x3f"))
        assertTrue(first.triggerPolicy().contains("maxpm=120"))
    }

    @Test
    fun customRequiresEverySettingAndRejectsUnsafeDependencies() {
        val dsl = artTi().apply { mode.set(ArtTiMode.CUSTOM) }
        val missing = assertThrows(GradleException::class.java) {
            EffectiveArtTiConfigResolver.resolve(dsl)
        }
        assertTrue(missing.message.orEmpty().contains("CUSTOM requires explicit"))

        dsl.garbageCollection.enabled.set(true)
        dsl.threads.lifecycle.set(false)
        dsl.threads.maxTrackedThreads.set(512)
        dsl.monitorContention.enabled.set(true)
        dsl.monitorContention.minDurationMs.set(8L)
        dsl.monitorContention.maxOpenIntervals.set(1024)
        dsl.stackSampling.enabled.set(false)
        dsl.stackSampling.maxDepth.set(64)
        dsl.stackSampling.onMainThreadStall.set(false)
        dsl.stackSampling.onLongContention.set(false)
        dsl.stackSampling.minTriggerIntervalMs.set(250L)
        dsl.stackSampling.maxSamplesPerMinute.set(120)
        dsl.stackSampling.maxStackDefinitions.set(1024)
        dsl.stackSampling.maxMethodDefinitions.set(4096)
        dsl.transport.capacity.set(4096)
        dsl.transport.drainBatchSize.set(256)
        dsl.transport.overflowPolicy.set(ArtTiOverflowPolicy.DROP_AND_COUNT)

        val invalid = assertThrows(GradleException::class.java) {
            EffectiveArtTiConfigResolver.resolve(dsl)
        }
        assertTrue(invalid.message.orEmpty().contains("threads.lifecycle"))
    }

    @Test
    fun rejectsNonPowerOfTwoTransportAndLightStackWalking() {
        val dsl = artTi().apply {
            mode.set(ArtTiMode.CAUSAL)
            transport.capacity.set(3000)
        }
        assertThrows(GradleException::class.java) { EffectiveArtTiConfigResolver.resolve(dsl) }

        dsl.mode.set(ArtTiMode.LIGHT)
        dsl.transport.capacity.set(4096)
        dsl.stackSampling.enabled.set(true)
        assertThrows(GradleException::class.java) { EffectiveArtTiConfigResolver.resolve(dsl) }
    }

    private fun artTi(): JankHunterExtension.ArtTi {
        return ProjectBuilder.builder().build().objects.newInstance(JankHunterExtension.ArtTi::class.java)
    }
}
