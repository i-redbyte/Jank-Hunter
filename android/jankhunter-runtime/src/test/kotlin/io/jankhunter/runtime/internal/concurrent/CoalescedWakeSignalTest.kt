package io.jankhunter.runtime.internal.concurrent

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CoalescedWakeSignalTest {
    @Test
    fun repeatedProducerSignalsCoalesceUntilTheConsumerClearsThem() {
        val signal = CoalescedWakeSignal()

        assertTrue(signal.tryRequest())
        repeat(10_000) { assertFalse(signal.tryRequest()) }
        signal.clear()
        assertTrue(signal.tryRequest())
    }
}
