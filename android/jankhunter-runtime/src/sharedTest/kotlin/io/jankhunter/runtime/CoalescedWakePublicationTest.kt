package io.jankhunter.runtime

import io.jankhunter.runtime.internal.concurrent.CoalescedWakeSignal
import java.util.concurrent.CyclicBarrier
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class CoalescedWakePublicationTest {
    @Test
    fun clearingAnOldSignalCannotHideANewReleasePublication() {
        val signal = CoalescedWakeSignal()
        val published = AtomicLong()
        val barrier = CyclicBarrier(2)
        val failure = AtomicReference<Throwable?>()
        var signalled = false
        val publisher = Thread {
            try {
                repeat(ROUNDS) {
                    barrier.await(5L, TimeUnit.SECONDS)
                    published.lazySet(1L)
                    signalled = signal.tryRequest()
                    barrier.await(5L, TimeUnit.SECONDS)
                }
            } catch (error: Throwable) {
                failure.set(error)
                barrier.reset()
            }
        }
        var missed = 0
        publisher.start()
        try {
            repeat(ROUNDS) {
                published.set(0L)
                signal.tryRequest() // A signal left over from the preceding drain.
                barrier.await(5L, TimeUnit.SECONDS)
                signal.clear()
                val observed = published.get()
                barrier.await(5L, TimeUnit.SECONDS)
                if (observed == 0L && !signalled) missed++
            }
        } finally {
            publisher.join(5_000L)
            if (publisher.isAlive) publisher.interrupt()
        }
        failure.get()?.let { throw it }
        assertFalse("publisher did not finish", publisher.isAlive)
        assertEquals("release publication and wake request were both missed", 0, missed)
    }

    private companion object {
        const val ROUNDS = 200_000
    }
}
