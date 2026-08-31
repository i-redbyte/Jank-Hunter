package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.TimeUnit

class AsyncControlLaneTest {
    @Test
    fun boundedLaneTracksSubmissionAndCompletionWithoutQueueNodeGrowth() {
        val lane = AsyncControlLane(capacity = 1)
        val first = AsyncControlRequest(targetSequence = 7L, writeLogGrowth = false, blocking = true)
        val rejected = AsyncControlRequest(targetSequence = 8L, writeLogGrowth = false, blocking = false)

        lane.beginSubmission()
        assertTrue(lane.hasSubmitters())
        assertTrue(lane.offer(first))
        assertFalse(lane.offer(rejected))
        lane.finishSubmission()

        assertFalse(lane.hasSubmitters())
        assertSame(first, lane.peek())
        assertSame(first, lane.poll())
        assertFalse(lane.hasPending())

        first.complete(success = true)
        assertTrue(first.await(TimeUnit.MILLISECONDS.toNanos(1L)))
        assertTrue(first.succeeded)
    }
}
