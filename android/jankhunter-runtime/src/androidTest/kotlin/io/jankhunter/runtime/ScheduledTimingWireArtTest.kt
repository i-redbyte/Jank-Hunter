package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.ExecutionException
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class ScheduledTimingWireArtTest {
    @Test
    fun publicScheduledExecutorPublishesSeparateTimingForEveryRun() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "scheduled-timing-wire")
        JankHunter.shutdown()
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(true).build()
        val delegate = Executors.newSingleThreadScheduledExecutor()
        fun tracked(name: String): ScheduledExecutorService = checkNotNull(
            JankHunterTelemetry.wrapScheduledExecutorService(delegate, name, null),
        )
        val executions = AtomicInteger()
        try {
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
            val once = tracked("once").schedule({ executions.incrementAndGet() }, 40L, TimeUnit.MILLISECONDS)
            val callable = tracked("callable").schedule(Callable { executions.incrementAndGet(); "result" }, 80L, TimeUnit.MILLISECONDS)
            val stop = IllegalStateException("scheduled fixture complete")
            val rateRuns = AtomicInteger()
            val delayRuns = AtomicInteger()
            val rate = tracked("rate").scheduleAtFixedRate({
                executions.incrementAndGet()
                if (rateRuns.incrementAndGet() == 4) throw stop
            }, 60L, 20L, TimeUnit.MILLISECONDS)
            val delay = tracked("delay").scheduleWithFixedDelay({
                executions.incrementAndGet()
                if (delayRuns.incrementAndGet() == 4) throw stop
            }, 60L, 20L, TimeUnit.MILLISECONDS)
            val cancelled = tracked("cancelled").schedule({ fail("cancelled task executed") }, 1L, TimeUnit.DAYS)
            assertTrue(cancelled.cancel(false))
            for ((name, samples, value) in listOf(
                Triple("wide_two", 2, Long.MAX_VALUE),
                Triple("wide_three", 3, Long.MAX_VALUE),
                Triple("wide_zero_low", 4, 1L shl 62),
            )) {
                val executor = tracked(name)
                repeat(samples) {
                    assertTrue(executor.schedule({ fail("huge delay executed") }, value, TimeUnit.MILLISECONDS).cancel(false))
                }
            }
            once.get(5L, TimeUnit.SECONDS)
            assertEquals("result", callable.get(5L, TimeUnit.SECONDS))
            for (future in listOf(rate, delay)) {
                try {
                    future.get(5L, TimeUnit.SECONDS)
                    fail("periodic task did not preserve its terminal failure")
                } catch (failure: ExecutionException) {
                    assertSame(stop, failure.cause)
                }
            }
            assertEquals(10, executions.get())
            JankHunter.flush()
        } finally {
            delegate.shutdownNow()
            assertTrue(delegate.awaitTermination(1L, TimeUnit.SECONDS))
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "scheduled-timing-5.1.0.jhlog"), overwrite = true)
    }
}
