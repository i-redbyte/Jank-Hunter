package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MethodCounterAccumulatorTest {
    @Test
    fun drainUsesPrimitiveConsumerPort() {
        val drain = MethodCounterAccumulator::class.java.declaredMethods.single { it.name == "drain" }

        assertFalse(drain.parameterTypes.contains(Function3::class.java))
    }

    @Test
    fun primitiveIdsAggregateWithoutBoxingAndCapacityRemainsHard() {
        val accumulator = MethodCounterAccumulator(maxEntries = 2)

        assertTrue(accumulator.add(11L, "first"))
        assertTrue(accumulator.add(11L, "first"))
        assertTrue(accumulator.add(0L, "zero-id"))
        assertFalse(accumulator.add(12L, "overflow"))
        assertEquals(2, accumulator.size)
        assertEquals(3L, accumulator.logicalEventCount())

        val drained = ArrayList<Long>(2)
        accumulator.drain { _, _, count -> drained.add(count) }

        assertEquals(listOf(2L, 1L), drained.sortedDescending())
        assertEquals(0, accumulator.size)
        assertEquals(0L, accumulator.logicalEventCount())
    }
}
