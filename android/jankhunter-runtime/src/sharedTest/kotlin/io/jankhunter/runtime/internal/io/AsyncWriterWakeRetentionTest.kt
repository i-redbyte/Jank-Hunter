package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import java.util.concurrent.Semaphore
import org.junit.Assert.*
import org.junit.Test

class AsyncWriterWakeRetentionTest {
    @Test
    fun delayedWakeForAnAlreadyConsumedEventCannotSuppressTheNextPublication() {
        val producer = AsyncWriterProducer(JankHunterConfig.builder().build())
        val semaphore = object : Semaphore(0) {
            override fun drainPermits(): Int {
                // A producer can be suspended between publishing an event and requesting its
                // wake. The worker may consume that event before this late notification arrives.
                producer.requestWorkerWake()
                return super.drainPermits()
            }
        }
        AsyncWriterProducer::class.java.getDeclaredField("queuedEvents").apply { isAccessible = true }
            .set(producer, semaphore)
        producer.prepareWorkerWait()
        producer.requestWorkerWake() // The next event arrives after the worker's second empty poll.
        assertTrue("a consumed late notification suppressed the next event wake", semaphore.tryAcquire())
    }
}
