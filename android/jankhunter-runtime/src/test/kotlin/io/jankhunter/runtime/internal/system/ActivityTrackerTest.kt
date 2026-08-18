package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Test

class ActivityTrackerTest {
    @Test
    fun trackerRemainsBoundedAcrossHighActivityCardinality() {
        val registry = BoundedRegistry<Int, Boolean>(MAX_TRACKED_ACTIVITIES) { inactive -> inactive }
        var cardinalityLosses = 0

        repeat(HIGH_CARDINALITY_ACTIVITY_COUNT) { index ->
            if (registry.makeRoomFor(index) != null) cardinalityLosses++
            registry[index] = true
        }

        assertEquals(MAX_TRACKED_ACTIVITIES, registry.size())
        assertEquals(HIGH_CARDINALITY_ACTIVITY_COUNT - MAX_TRACKED_ACTIVITIES, cardinalityLosses)
    }

    private companion object {
        const val HIGH_CARDINALITY_ACTIVITY_COUNT = 10_000
        const val MAX_TRACKED_ACTIVITIES = 64
    }
}
