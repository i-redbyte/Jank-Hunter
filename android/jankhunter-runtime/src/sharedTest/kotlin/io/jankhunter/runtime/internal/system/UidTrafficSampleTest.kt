package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UidTrafficSampleTest {
    @Test
    fun unsupportedAndSupportedZeroHaveDifferentKnownness() {
        for (uid in intArrayOf(0, 10000, Int.MAX_VALUE)) {
            for (flags in 0..3) {
                val rx = if (flags and 1 != 0) 0L else -1L
                val tx = if (flags and 2 != 0) 0L else -1L
                val sample = UidTrafficSample.from(uid, rx, tx)
                assertEquals(uid.toLong() + 1L, sample.uidPlusOne)
                assertEquals(flags, sample.knownFlags)
                assertEquals(0L, sample.rxBytes)
                assertEquals(0L, sample.txBytes)
            }
        }
        assertEquals(0, UidTrafficSample.from(-1, 12L, 34L).knownFlags)
        assertEquals(0L, UidTrafficSample.from(-1, 12L, 34L).uidPlusOne)
        assertEquals(Long.MAX_VALUE, UidTrafficSample.from(1, Long.MAX_VALUE, 0L).rxBytes)
    }

    @Test
    fun adaptiveSamplingNeverHidesKnownnessOrUidTransition() {
        val sampler = AdaptiveRuntimeSampler(60_000L, 60_000L)
        fun observe(uid: Long, flags: Int) = sampler.shouldRecordContext(
            1L, 0, 50, 100L, false, false, false, 0L, 0L, false, uid, flags,
        )
        assertTrue(observe(1L, 0))
        assertFalse(observe(1L, 0))
        for (flags in intArrayOf(1, 3, 2, 0)) {
            assertTrue(observe(1L, flags))
            assertFalse(observe(1L, flags))
        }
        assertTrue(observe(2L, 0))
        assertFalse(observe(2L, 0))
    }
}
