package io.jankhunter.runtime.internal.io

import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

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

        assertTrue(first.tryClaim())
        first.complete(success = true)
        assertTrue(first.await(TimeUnit.MILLISECONDS.toNanos(1L)))
        assertTrue(first.succeeded)
    }

    @Test
    fun requestLifecycleUsesPrimitiveStateInsteadOfAllocatingAnAtomicWrapper() {
        assertTrue(AsyncControlRequest::class.java.declaredFields.none { field ->
            field.type == AtomicInteger::class.java
        })
    }

    @Test
    fun completionObserverDoesNotSwallowFatalVmFailures() {
        val fatal = TestVirtualMachineError()
        val request = AsyncControlRequest(
            targetSequence = 0L,
            writeLogGrowth = false,
            blocking = true,
            onCompleted = { throw fatal },
        )
        assertTrue(request.tryClaim())

        try {
            request.complete(success = true)
            fail("fatal completion failure was swallowed")
        } catch (actual: TestVirtualMachineError) {
            assertSame(fatal, actual)
        }
    }

    private class TestVirtualMachineError : VirtualMachineError()
}
