package io.jankhunter.artti.internal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ArtTiRuntimeConfigTest {
    @Test
    fun parsesVersionedNativeAndTriggerConfig() {
        val config = ArtTiRuntimeConfigParser.parse(NATIVE_OPTIONS, TRIGGER_POLICY).getOrThrow()

        assertEquals(2, config.native.profile)
        assertEquals(4096, config.native.transportCapacity)
        assertEquals(64, config.native.maxStackDepth)
        assertEquals(250, config.native.minStackTriggerIntervalMs)
        assertEquals(120, config.native.maxStackSamplesPerMinute)
        assertEquals(0x3fL, config.native.requestedCapabilities)
        assertTrue(config.triggerPolicy.onMainThreadStall)
        assertTrue(config.triggerPolicy.onLongContention)
        assertEquals(50L, config.triggerPolicy.drainIntervalMs)
    }

    @Test
    fun rejectsMissingDuplicateAndMismatchedPolicy() {
        assertTrue(ArtTiRuntimeConfigParser.parse(null, TRIGGER_POLICY).isFailure)
        assertTrue(ArtTiRuntimeConfigParser.parse("$NATIVE_OPTIONS;cap=1", TRIGGER_POLICY).isFailure)
        assertTrue(
            ArtTiRuntimeConfigParser.parse(
                NATIVE_OPTIONS,
                "v=1;main=1;long=1;minms=100;maxpm=120;drainms=50",
            ).isFailure,
        )
    }

    @Test
    fun triggerBudgetIsWindowedAndThreadContextTableIsBounded() {
        val budget = ArtTiTriggerBudget(minIntervalMs = 250L, maxPerMinute = 2)
        assertTrue(budget.tryAcquire(1_000L))
        assertFalse(budget.tryAcquire(1_100L))
        assertTrue(budget.tryAcquire(1_250L))
        assertFalse(budget.tryAcquire(2_000L))
        assertTrue(budget.tryAcquire(61_001L))

        val contexts = ArtTiThreadContextTable(4)
        assertTrue(contexts.put(1L, 10L))
        assertTrue(contexts.put(5L, 50L))
        assertEquals(10L, contexts.get(1L))
        assertTrue(contexts.remove(1L))
        assertEquals(0L, contexts.get(1L))
    }

    private companion object {
        const val NATIVE_OPTIONS =
            "v=1;profile=2;transport=4096;threads=512;contentions=1024;depth=64;" +
                "stackdefs=1024;methoddefs=4096;triggerms=250;samplespm=120;batch=256;" +
                "mincontentionns=8000000;hash=0x44df3fe44304d5de;cap=0x3f"
        const val TRIGGER_POLICY = "v=1;main=1;long=1;minms=250;maxpm=120;drainms=50"
    }
}
