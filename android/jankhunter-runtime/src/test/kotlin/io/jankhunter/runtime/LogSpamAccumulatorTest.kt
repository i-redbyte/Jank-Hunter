package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class LogSpamAccumulatorTest {
    @Test
    fun drainUsesPrimitiveConsumerPort() {
        val drain = LogSpamAccumulator::class.java.declaredMethods.single { it.name == "drain" }

        assertFalse(drain.parameterTypes.contains(Function6::class.java))
    }

    @Test
    fun repeatedKeysAggregateWithoutGrowingAndCapacityRemainsHard() {
        val accumulator = LogSpamAccumulator(maxEntries = 2)

        assertTrue(accumulator.add("screen", "owner", "source", 3, 7L))
        assertTrue(accumulator.add("screen", "owner", "source", 3, 7L))
        assertTrue(accumulator.add(null, null, "other", 4, 0L))
        assertFalse(accumulator.add("overflow", null, null, 5, 0L))
        assertEquals(2, accumulator.size)
        assertEquals(3L, accumulator.logicalEventCount())

        val drained = ArrayList<Long>(2)
        accumulator.drain { _, _, _, _, _, count -> drained.add(count) }

        assertEquals(listOf(2L, 1L), drained.sortedDescending())
        assertEquals(0, accumulator.size)
        assertEquals(0L, accumulator.logicalEventCount())
    }
}
