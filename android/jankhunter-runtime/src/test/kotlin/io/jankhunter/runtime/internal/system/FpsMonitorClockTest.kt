package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeCollectorCallbacks
import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.internal.io.Jhlog
import java.lang.reflect.Proxy
import org.junit.Assert.assertEquals
import org.junit.Test

class FpsMonitorClockTest {
    @Test
    fun stopUsesFrameTimeBaseAfterDeepSleep() {
        val fixture = Fixture()
        fixture.fallbackFrames()
        fixture.monitor.stop()
        fixture.assertWindow(0, 32L, 2L, Jhlog.UI_SOURCE_CHOREOGRAPHER)
    }

    @Test
    fun changingSourceAndWindowKeepsEachPartialFrameWindowOnItsOwnTimeBase() {
        val fixture = Fixture()
        fixture.fallbackFrames()
        fixture.monitor.setJankStatsActive(true)
        fixture.assertWindow(0, 32L, 2L, Jhlog.UI_SOURCE_CHOREOGRAPHER)
        fixture.uptimeNanos += 16_000_000L
        fixture.monitor.onJankStatsFrame("screen", 16_000_000L, false)
        // Deep sleep does not advance the frame clock; only the next 16 ms of awake time does.
        fixture.uptimeNanos += 16_000_000L
        fixture.monitor.onJankStatsFrame("screen", 16_000_000L, true)
        fixture.monitor.setJankStatsActive(false)
        fixture.assertWindow(1, 32L, 2L, Jhlog.UI_SOURCE_JANKSTATS)
        fixture.fallbackFrames()
        fixture.monitor.setWindowActive(false)
        fixture.assertWindow(2, 32L, 2L, Jhlog.UI_SOURCE_CHOREOGRAPHER)
        fixture.monitor.stop()
        assertEquals(3, fixture.windows.size)
    }

    @Test
    fun pausingJankStatsWindowPreservesItsSource() {
        val fixture = Fixture()
        fixture.monitor.setJankStatsActive(true)
        fixture.monitor.onJankStatsFrame("screen", 16_000_000L, false)
        fixture.monitor.setWindowActive(false)
        fixture.assertWindow(0, 16L, 1L, Jhlog.UI_SOURCE_JANKSTATS)
        fixture.monitor.stop()
    }

    private class Fixture {
        var uptimeNanos = 1_000_000_000L
        val windows = mutableListOf<Array<out Any?>>()
        val monitor = FpsMonitor(
            windowMs = 1_000L,
            jankFrameThresholdMs = 16L,
            callbacks = callbacks(),
            exactAdmission = true,
            mainThread = object : FpsMainThreadDispatcher {
                override fun isMainThread(): Boolean = true
                override fun post(task: Runnable): Boolean {
                    task.run()
                    return true
                }
            },
            nanoTime = RuntimeLongSource { uptimeNanos },
        )

        init {
            monitor.start()
            monitor.setWindowActive(true)
        }

        fun fallbackFrames() {
            monitor.doFrame(uptimeNanos)
            repeat(2) {
                uptimeNanos += 16_000_000L
                monitor.doFrame(uptimeNanos)
            }
        }

        fun assertWindow(index: Int, elapsedMs: Long, frames: Long, source: Long) {
            val window = windows[index]
            assertEquals("window duration", elapsedMs, window[1])
            assertEquals("frame count", frames, window[2])
            assertEquals("frame source", source, window[5])
        }

        private fun callbacks(): RuntimeCollectorCallbacks {
            val proxy = Proxy.newProxyInstance(
                RuntimeCollectorCallbacks::class.java.classLoader,
                arrayOf(RuntimeCollectorCallbacks::class.java),
            ) { _, method, arguments ->
                if (method.name == "recordUiWindow") windows.add(checkNotNull(arguments))
                when (method.returnType) {
                    java.lang.Boolean.TYPE -> false
                    java.lang.Integer.TYPE -> 0
                    java.lang.Long.TYPE -> 0L
                    String::class.java -> "screen"
                    else -> null
                }
            }
            return checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy))
        }
    }
}
