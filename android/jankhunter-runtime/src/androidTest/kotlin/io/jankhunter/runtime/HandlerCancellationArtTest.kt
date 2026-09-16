package io.jankhunter.runtime

import android.os.Handler
import android.os.HandlerThread
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HandlerCancellationArtTest {
    @Test
    fun platformHasCallbacksUsesTheOriginalRunnableIdentity() = Fixture().use { fixture ->
        val calls = AtomicInteger()
        val original = Runnable { calls.incrementAndGet() }
        fixture.post(original)
        assertTrue("platform cannot find the application's callback", fixture.handler.hasCallbacks(original))
        fixture.handler.removeCallbacks(original)
        assertFalse(fixture.handler.hasCallbacks(original))
        fixture.drain()
        assertEquals(0, calls.get())
    }

    @Test
    fun repeatedPostsRemainScopedByHandlerAndToken() = Fixture().use { fixture ->
        val calls = AtomicInteger()
        val original = Runnable { calls.incrementAndGet() }
        val tokenA = Any()
        val tokenB = Any()
        val other = Handler(fixture.handler.looper)
        fixture.post(original, tokenA)
        fixture.post(original, tokenB)
        fixture.post(original, tokenA, other)
        fixture.handler.removeCallbacks(original, tokenA)
        fixture.drain()
        assertEquals("cancellation must remove only the matching Handler/token pair", 2, calls.get())
    }

    @Test
    fun cancellationSurvivesTrackingClearAndRuntimeDisable() {
        for (disable in booleanArrayOf(false, true)) Fixture().use { fixture ->
            val calls = AtomicInteger()
            val original = Runnable { calls.incrementAndGet() }
            repeat(3) { fixture.post(original) }
            fixture.graph.handlerHooks.clear()
            if (disable) fixture.graph.state.featureGate.deactivate()
            fixture.handler.removeCallbacks(original)
            fixture.drain()
            assertEquals("clearing telemetry changed platform cancellation", 0, calls.get())
        }
    }

    @Test
    fun removeCallbacksAndMessagesKeepsOtherTokensAndHandlers() = Fixture().use { fixture ->
        val calls = AtomicInteger()
        val original = Runnable { calls.incrementAndGet() }
        val tokenA = Any()
        val tokenB = Any()
        val other = Handler(fixture.handler.looper)
        fixture.post(original, tokenA)
        fixture.post(original, tokenB)
        fixture.post(original, tokenA, other)
        fixture.handler.removeCallbacksAndMessages(tokenA)
        fixture.drain()
        assertEquals(2, calls.get())
    }

    @Test
    fun cancellationBetweenTwoPostsLeavesOnlyTheLaterSubmission() = Fixture().use { fixture ->
        val calls = AtomicInteger()
        val original = Runnable { calls.incrementAndGet() }
        fixture.post(original)
        val cancelled = CountDownLatch(1)
        val producer = Thread {
            check(cancelled.await(5L, TimeUnit.SECONDS))
            fixture.post(original)
        }
        producer.start()
        fixture.handler.removeCallbacks(original)
        cancelled.countDown()
        producer.join(1_000L)
        assertFalse(producer.isAlive)
        fixture.drain()
        assertEquals("cancel/post order must remain the platform's order", 1, calls.get())
    }

    @Test
    fun uninstrumentedRemoveCallbacksCancelsAnInstrumentedPost() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val directory = context.cacheDir.resolve("handler-cancel-${System.nanoTime()}")
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "instrumentation")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.writer = writer
        graph.state.config = config
        graph.coordinator.markStarted(config)
        val thread = HandlerThread("JHCancelContract").apply { start() }
        val handler = Handler(thread.looper)
        val blocked = CountDownLatch(1)
        val release = CountDownLatch(1)
        val drained = CountDownLatch(1)
        val executions = AtomicInteger()
        val original = Runnable { executions.incrementAndGet() }
        try {
            assertTrue(handler.post {
                blocked.countDown()
                check(release.await(5L, TimeUnit.SECONDS))
            })
            assertTrue(blocked.await(1L, TimeUnit.SECONDS))
            val posted = graph.handlerHooks.wrap(handler, original, null, "ArtOwner")
            assertTrue(handler.post(posted))
            // This call deliberately goes straight to the platform, like an uninstrumented library.
            handler.removeCallbacks(original)
            assertTrue(handler.post { drained.countDown() })
            release.countDown()
            assertTrue(drained.await(1L, TimeUnit.SECONDS))
            assertEquals("SDK changed Runnable identity seen by platform cancellation", 0, executions.get())
        } finally {
            release.countDown()
            handler.removeCallbacksAndMessages(null)
            thread.quitSafely()
            thread.join(1_000L)
            assertFalse(thread.isAlive)
            graph.session.stop(clearInit = true)
            writer.close()
            directory.deleteRecursively()
        }
    }

    private class Fixture : AutoCloseable {
        private val context = InstrumentationRegistry.getInstrumentation().targetContext
        private val directory = context.cacheDir.resolve("handler-contract-${System.nanoTime()}")
        private val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        private val writer = AsyncLogWriterFactory().open(directory, config, "handler-contract")
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        private val thread = HandlerThread("HandlerContract").apply { start() }
        val handler = Handler(thread.looper)
        private val release = CountDownLatch(1)

        init {
            graph.state.writer = writer
            graph.state.config = config
            graph.coordinator.markStarted(config)
            val entered = CountDownLatch(1)
            check(handler.post {
                entered.countDown()
                check(release.await(5L, TimeUnit.SECONDS))
            })
            check(entered.await(1L, TimeUnit.SECONDS))
        }

        fun post(original: Runnable, token: Any? = null, target: Handler = handler) {
            val queued = graph.handlerHooks.wrap(target, original, token, "ContractOwner")
            val posted = target.postAtTime(queued, token, SystemClock.uptimeMillis())
            graph.handlerHooks.onPostResult(original, queued, posted)
            check(posted)
        }

        fun drain() {
            val done = CountDownLatch(1)
            check(handler.post { done.countDown() })
            release.countDown()
            check(done.await(1L, TimeUnit.SECONDS))
        }

        override fun close() {
            release.countDown()
            handler.removeCallbacksAndMessages(null)
            thread.quitSafely()
            thread.join(1_000L)
            assertFalse(thread.isAlive)
            graph.session.stop(clearInit = true)
            writer.close()
            directory.deleteRecursively()
        }
    }
}
