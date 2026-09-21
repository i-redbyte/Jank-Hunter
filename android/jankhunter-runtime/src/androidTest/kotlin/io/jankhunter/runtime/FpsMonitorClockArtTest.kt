package io.jankhunter.runtime

import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import io.jankhunter.runtime.internal.io.Jhlog
import io.jankhunter.runtime.internal.system.FpsMonitor
import java.io.File
import java.lang.reflect.Proxy
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class FpsMonitorClockArtTest {
    @Test
    fun partialWindowsKeepDurationAndSourceThroughAndroidWriter() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.targetContext.cacheDir, "fps-clock-${System.nanoTime()}")
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val graph = RuntimeComponentGraph(
            nowMs = { SystemClock.elapsedRealtime() },
            nowUs = { SystemClock.elapsedRealtimeNanos() / 1_000L },
        )
        graph.state.config = config
        graph.state.featureGate.activate(config)
        graph.state.writer = writer
        val windows = mutableListOf<Array<out Any?>>()
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader,
            arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, arguments ->
            if (method.name == "recordUiWindow") windows.add(checkNotNull(arguments))
            method.invoke(graph.collectorTelemetry, *(arguments ?: emptyArray()))
        }
        val callbacks = checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy))
        try {
            instrumentation.runOnMainSync {
                var uptime = 1_000_000_000L
                val monitor = FpsMonitor(1_000L, 16L, callbacks, exactAdmission = true, nanoTime = { uptime })
                monitor.start()
                try {
                    monitor.setWindowActive(true)
                    monitor.doFrame(uptime)
                    repeat(2) {
                        uptime += 16_000_000L
                        monitor.doFrame(uptime)
                    }
                    monitor.setJankStatsActive(true)
                    repeat(2) {
                        uptime += 16_000_000L
                        monitor.onJankStatsFrame("art.fps", 16_000_000L, false)
                    }
                    monitor.setWindowActive(false)
                } finally {
                    assertTrue(monitor.stop())
                }
                assertEquals(2, windows.size)
                for (window in windows) {
                    assertEquals(32L, window[1])
                    assertEquals(2L, window[2])
                }
                assertEquals(Jhlog.UI_SOURCE_CHOREOGRAPHER, windows[0][5])
                assertEquals(Jhlog.UI_SOURCE_JANKSTATS, windows[1][5])

                // The default source must also share the platform Choreographer time base.
                val platformMonitor = FpsMonitor(1_000L, 16L, callbacks, exactAdmission = true)
                platformMonitor.start()
                try {
                    platformMonitor.setWindowActive(true)
                    val now = System.nanoTime()
                    platformMonitor.doFrame(now - 32_000_000L)
                    platformMonitor.doFrame(now - 16_000_000L)
                    platformMonitor.doFrame(now)
                } finally {
                    assertTrue(platformMonitor.stop())
                }
                assertEquals(3, windows.size)
                assertTrue("unexpected platform frame duration: ${windows[2][1]}", (windows[2][1] as Long) in 32L..100L)
            }
            assertTrue(writer.close(1_000L))
            val log = directory.walkTopDown().single { it.isFile && it.extension == "jhlog" }
            log.copyTo(File(instrumentation.context.filesDir, "fps-clock.jhlog"), overwrite = true)
        } finally {
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }
}
