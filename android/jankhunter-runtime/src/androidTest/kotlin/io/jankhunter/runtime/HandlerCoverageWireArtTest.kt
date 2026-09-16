package io.jankhunter.runtime

import android.os.Handler
import android.os.HandlerThread
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class HandlerCoverageWireArtTest {
    @Test
    fun publicLegacyHooksKeepIdentityAndPublishSuccessfulPostCoverage() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "handler-coverage-wire")
        JankHunter.shutdown()
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        val thread = HandlerThread("HandlerCoverageWire").apply { start() }
        val handler = Handler(thread.looper)
        val release = CountDownLatch(1)
        val blocked = CountDownLatch(1)
        val done = CountDownLatch(1)
        val executions = AtomicInteger()
        try {
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
            assertTrue(handler.post { blocked.countDown(); check(release.await(5L, TimeUnit.SECONDS)) })
            assertTrue(blocked.await(1L, TimeUnit.SECONDS))
            val original = Runnable { executions.incrementAndGet() }
            repeat(7) { index ->
                val queued = JankHunterHooks.wrapHandlerRunnable(handler, original, null, "Owner$index")
                assertSame(original, queued)
                val posted = handler.post(queued!!)
                JankHunterHooks.onHandlerPostResult(original, queued, posted)
                assertTrue(posted)
                if (index == 2) handler.removeCallbacks(original)
            }
            JankHunterHooks.onHandlerPostResult(original, original, false)
            val wrappers = JankHunterHooks.handlerWrappers(handler, original, null)
            assertEquals(0, wrappers.size)
            assertSame(wrappers, JankHunterHooks.handlerWrappers(handler, original, null))
            JankHunterHooks.clearHandlerWrappers(handler, original, null)
            JankHunterHooks.clearHandlerWrappers(handler, null)
            assertTrue(handler.hasCallbacks(original))
            assertTrue(handler.post { done.countDown() })
            release.countDown()
            assertTrue(done.await(1L, TimeUnit.SECONDS))
            assertEquals(4, executions.get())
            JankHunter.flush()
        } finally {
            release.countDown()
            handler.removeCallbacksAndMessages(null)
            thread.quitSafely()
            thread.join(1_000L)
            assertFalse(thread.isAlive)
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "handler-coverage-5.1.0.jhlog"), overwrite = true)
    }
}
