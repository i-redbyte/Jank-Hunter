package io.jankhunter.runtime

import android.app.Activity
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.TextView
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import androidx.metrics.performance.FrameData
import androidx.metrics.performance.JankStats
import io.jankhunter.runtime.integration.JankHunterJankStats
import io.jankhunter.runtime.internal.system.FpsMonitor
import java.lang.reflect.Proxy
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class JankStatsBridgeArtTest {
    @Test
    fun workerFramesAheadOfTheRealMainQueueStopMarkerAreFlushedOnce() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val frames = AtomicLong()
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader, arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, args ->
            if (method.name == "recordUiWindow") frames.addAndGet(checkNotNull(args)[2] as Long)
            null
        }
        val callbacks = checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy))
        lateinit var monitor: FpsMonitor
        instrumentation.runOnMainSync {
            monitor = FpsMonitor(Long.MAX_VALUE, 16L, callbacks, exactAdmission = true, nanoTime = { 1_000_000_000L })
            monitor.start()
            monitor.setJankStatsActive(true)
            monitor.setWindowActive(true)
        }
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val main = Handler(Looper.getMainLooper())
        assertTrue(main.post {
            entered.countDown()
            release.await(5, TimeUnit.SECONDS)
        })
        try {
            assertTrue(entered.await(2, TimeUnit.SECONDS))
            val listener = checkNotNull(JankHunterJankStats.createFrameListener { jank, duration ->
                monitor.onJankStatsFrame("queued-art", duration, jank)
            }) as JankStats.OnFrameListener
            val frame = FrameData(1L, 16_000_000L, false, emptyList())
            repeat(200) { listener.onFrame(frame) }
            assertFalse(monitor.stop(20L))
            release.countDown()
            instrumentation.runOnMainSync { assertEquals(200L, frames.get()) }
        } finally {
            release.countDown()
            instrumentation.runOnMainSync { monitor.stop() }
        }
    }

    @Test
    fun realAndroidXFramesSurviveRepeatedAttachAndDetach() {
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            repeat(3) {
                val frames = CountDownLatch(2)
                val invalidDurations = AtomicInteger()
                var handle: JankHunterJankStats.Handle? = null
                try {
                    scenario.onActivity { activity ->
                        handle = JankHunterJankStats.install(activity.window) { _, duration ->
                            if (duration < 0L) invalidDurations.incrementAndGet()
                            frames.countDown()
                        }
                        assertNotNull("JankStats installation failed", handle)
                        activity.drawFrames()
                    }
                    assertTrue("real JankStats frames were not delivered", frames.await(5, TimeUnit.SECONDS))
                    assertTrue("invalid frame duration", invalidDurations.get() == 0)
                } finally {
                    scenario.onActivity { handle?.uninstall() }
                }
            }
        }
    }
}

class JankStatsProbeActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(TextView(this).apply { text = "JankStats instrumentation probe" })
    }

    fun drawFrames() {
        val view = findViewById<View>(android.R.id.content)
        view.postOnAnimation(object : Runnable {
            private var remaining = 30
            override fun run() {
                view.invalidate()
                if (--remaining > 0 && !isFinishing) view.postOnAnimation(this)
            }
        })
    }
}
