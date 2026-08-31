package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterOperation
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class ProducerContextTrackerTest {
    @Test
    fun serializedContextDoesNotRetainOperationHandle() {
        assertTrue(LogEventContext::class.java.declaredFields.none { field ->
            JankHunterOperation::class.java.isAssignableFrom(field.type)
        })
    }

    @Test
    fun equivalentUpdateReusesProducerContext() {
        val tracker = ProducerContextTracker()
        tracker.update("screen", "owner", 3L)
        val initial = tracker.capture()

        tracker.update("screen", "owner", 3L)

        assertSame(initial, tracker.capture())
    }
}
