package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterLogSpamTest {
    @Test
    fun dropsUniqueSourcesAboveMaxKeyCapUnderConcurrency() {
        val config = JankHunterConfig.builder()
            .maxLogSpamKeys(4)
            .build()
        val dropped = AtomicInteger()
        val logSpam = RuntimeLogSpamService(
            nowMs = { 0L },
            config = { config },
            writer = { null },
            runtimeActive = { true },
            foreground = { true },
            captureContext = { JankHunterContext(null, null, null, null) },
            recordDropCounter = { dropped.incrementAndGet() },
        )
        val pool = Executors.newFixedThreadPool(64)
        try {
            val ready = CountDownLatch(64)
            val start = CountDownLatch(1)
            repeat(64) { index ->
                pool.execute {
                    ready.countDown()
                    assertTrue(start.await(2, TimeUnit.SECONDS))
                    logSpam.record("Owner-$index", "android.util.Log.d.$index", 3)
                }
            }

            assertTrue(ready.await(2, TimeUnit.SECONDS))
            start.countDown()
        } finally {
            pool.shutdown()
            assertTrue(pool.awaitTermination(2, TimeUnit.SECONDS))
        }

        assertTrue("expected keys above the cap to be dropped", dropped.get() > 0)
    }
}
