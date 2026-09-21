package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeCollectorCallbacks
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookFailureTracker
import java.lang.reflect.Proxy
import java.util.ArrayDeque
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class FpsQueuedFrameLifecycleTest {
    @Test
    fun callbackPublishedAfterTheStopMarkerCannotWriteIntoAClosedWindow() {
        val fixture = Fixture()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        fixture.dispatcher.beforePost = {
            entered.countDown()
            assertTrue(release.await(2, TimeUnit.SECONDS))
        }
        val before = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.JANKSTATS_FRAME)
        val worker = Executors.newSingleThreadExecutor()
        try {
            val callback = worker.submit { fixture.monitor.onJankStatsFrame("late", 17_000_000L, false) }
            assertTrue(entered.await(2, TimeUnit.SECONDS))
            fixture.dispatcher.onMain { assertTrue(fixture.monitor.stop()) }
            release.countDown()
            callback.get(2, TimeUnit.SECONDS)
            fixture.dispatcher.beforePost = null
            fixture.dispatcher.drain()
            assertTrue(fixture.windows.isEmpty())
            assertEquals(before + 1L, RuntimeHookFailureTracker.count(RuntimeHookFailureReason.JANKSTATS_FRAME))
        } finally {
            release.countDown()
            worker.shutdownNow()
            assertTrue(worker.awaitTermination(2, TimeUnit.SECONDS))
        }
    }

    @Test
    fun stopPreservesFramesAlreadyQueuedAheadOfItsFinalFlush() {
        val fixture = Fixture()
        fixture.monitor.onJankStatsFrame("first", 7_000_000L, false)
        fixture.monitor.onJankStatsFrame("second", 23_000_000L, true)
        assertFalse(fixture.monitor.stop(0L))
        fixture.dispatcher.drain()
        assertEquals(listOf("first", "second"), fixture.windows.map { it[0] })
        assertEquals(listOf(1L, 1L), fixture.windows.map { it[2] })
        assertEquals(listOf(7L, 23L), fixture.windows.map { it[4] })
        fixture.monitor.onJankStatsFrame("after-stop", 99_000_000L, true)
        fixture.dispatcher.drain()
        assertEquals(2, fixture.windows.size)
    }

    private class Fixture {
        val windows = mutableListOf<Array<out Any?>>()
        val dispatcher = QueuedDispatcher()
        val monitor = FpsMonitor(
            Long.MAX_VALUE, 16L, callbacks(), exactAdmission = true,
            mainThread = dispatcher, nanoTime = { 1_000_000_000L },
        )
        init {
            monitor.start()
            monitor.setJankStatsActive(true)
            monitor.setWindowActive(true)
            dispatcher.drain()
        }
        private fun callbacks(): RuntimeCollectorCallbacks {
            val proxy = Proxy.newProxyInstance(
                RuntimeCollectorCallbacks::class.java.classLoader,
                arrayOf(RuntimeCollectorCallbacks::class.java),
            ) { _, method, args ->
                if (method.name == "recordUiWindow") windows.add(checkNotNull(args))
                null
            }
            return checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy))
        }
    }

    private class QueuedDispatcher : FpsMainThreadDispatcher {
        private val queue = ArrayDeque<Runnable>()
        private var onMain = false
        var beforePost: (() -> Unit)? = null
        override fun isMainThread(): Boolean = onMain
        override fun post(task: Runnable): Boolean {
            beforePost?.invoke()
            return queue.add(task)
        }
        fun onMain(block: () -> Unit) {
            assertFalse(onMain)
            onMain = true
            try {
                block()
            } finally {
                onMain = false
            }
        }
        fun drain() {
            onMain { while (queue.isNotEmpty()) queue.removeFirst().run() }
            assertTrue(queue.isEmpty())
        }
    }
}
