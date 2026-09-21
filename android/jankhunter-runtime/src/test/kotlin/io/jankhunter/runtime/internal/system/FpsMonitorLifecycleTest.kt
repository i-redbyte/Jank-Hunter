package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeCollectorCallbacks
import java.lang.reflect.Proxy
import org.junit.Assert.assertEquals
import org.junit.Test

class FpsMonitorLifecycleTest {
    @Test
    fun rejectedInitialMainThreadDispatchAllowsStartRetry() {
        val mainThread = FakeFpsMainThreadDispatcher(acceptPost = false)
        val monitor = FpsMonitor(
            windowMs = 1_000L,
            jankFrameThresholdMs = 16L,
            callbacks = noOpCallbacks(),
            mainThread = mainThread,
        )

        monitor.start()
        mainThread.acceptPost = true
        monitor.start()

        assertEquals(2, mainThread.postCount)
        monitor.stop()
    }

    @Test
    fun throwingInitialMainThreadDispatchAllowsStartRetry() {
        val mainThread = FakeFpsMainThreadDispatcher(acceptPost = true, throwOnPost = true)
        val monitor = FpsMonitor(
            windowMs = 1_000L,
            jankFrameThresholdMs = 16L,
            callbacks = noOpCallbacks(),
            mainThread = mainThread,
        )

        monitor.start()
        mainThread.throwOnPost = false
        monitor.start()

        assertEquals(2, mainThread.postCount)
        monitor.stop()
    }

    @Test
    fun frameDeadlineConversionSaturatesInsteadOfBecomingNegative() {
        assertEquals(Long.MAX_VALUE, millisecondsToMicroseconds(Long.MAX_VALUE))
        assertEquals(32_000L, millisecondsToMicroseconds(32L))
    }

    private fun noOpCallbacks(): RuntimeCollectorCallbacks {
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader,
            arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, _ ->
            when (method.returnType) {
                java.lang.Boolean.TYPE -> false
                java.lang.Integer.TYPE -> 0
                java.lang.Long.TYPE -> 0L
                String::class.java -> ""
                else -> null
            }
        }
        return checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy))
    }

    private class FakeFpsMainThreadDispatcher(
        var acceptPost: Boolean,
        var throwOnPost: Boolean = false,
    ) : FpsMainThreadDispatcher {
        var postCount = 0

        override fun isMainThread(): Boolean = false

        override fun post(task: Runnable): Boolean {
            postCount++
            if (throwOnPost) error("main queue unavailable")
            return acceptPost
        }
    }
}
