package io.jankhunter.runtime.internal.io

import java.io.IOException
import java.util.concurrent.CountDownLatch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncWriterLifecycleTest {
    @Test
    fun terminalReasonAndFailureAreCommittedAsOneState() {
        val lifecycle = AsyncWriterLifecycle()

        lifecycle.recordTermination(reason = 7)
        lifecycle.recordTermination(reason = 9, failure = IOException("late failure"))

        assertEquals(7, lifecycle.terminalReason())
        assertNull(lifecycle.terminalFailure())
    }

    @Test
    fun concurrentTerminationCannotMixReasonAndFailureFromDifferentWriters() {
        repeat(100) {
            val lifecycle = AsyncWriterLifecycle()
            val start = CountDownLatch(1)
            val threads = (1..2).map { reason ->
                Thread {
                    start.await()
                    lifecycle.recordTermination(reason, IOException(reason.toString()))
                }.apply(Thread::start)
            }

            start.countDown()
            threads.forEach(Thread::join)

            val reason = lifecycle.terminalReason()
            assertTrue(reason == 1 || reason == 2)
            assertEquals(reason.toString(), lifecycle.terminalFailure()?.message)
        }
    }

    @Test
    fun lifecycleOwnsOneAtomicTerminationTuple() {
        val fields = AsyncWriterLifecycle::class.java.declaredFields.mapTo(HashSet()) { field -> field.name }

        assertTrue("termination" in fields)
        assertTrue("terminalReason" !in fields)
        assertTrue("terminalFailure" !in fields)
    }
}
