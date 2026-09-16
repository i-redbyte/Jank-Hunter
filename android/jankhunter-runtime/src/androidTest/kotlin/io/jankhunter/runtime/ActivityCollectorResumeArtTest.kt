package io.jankhunter.runtime

import android.app.Activity
import android.app.Application
import android.os.Bundle
import android.os.SystemClock
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.system.FpsMonitor
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.FrameSourceSelector
import io.jankhunter.runtime.internal.system.RuntimeMainThreadDispatcher
import java.io.File
import java.lang.reflect.Proxy
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ActivityCollectorResumeArtTest {
    @Test
    fun restoringCurrentStateDoesNotInventLifecycleEventsOrScreenOpenOperations() {
        val events = mutableListOf<String>()
        val gauges = mutableMapOf<String, Long>()
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader, arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, args ->
            when (method.name) {
                "startScreenOpenOperation" -> events.add(method.name)
                "recordCounter" -> events.add(args!![0] as String)
                "recordGauge" -> gauges[args!![0] as String] = args[1] as Long
            }
            when (method.returnType) {
                java.lang.Boolean.TYPE -> false
                java.lang.Long.TYPE -> 0L
                java.lang.Integer.TYPE -> 0
                String::class.java -> ""
                else -> null
            }
        }
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                val tracker = ActivityTracker(RuntimeCollectorCallbacks::class.java.cast(proxy)!!)
                try {
                    tracker.restoreObservedActivity(activity, resumed = true)
                    tracker.restoreObservedActivity(activity, resumed = true)
                    assertTrue("restore invented past events: $events", events.isEmpty())
                    assertEquals(mapOf("app.lifecycle.started_activities" to 1L), gauges)
                    tracker.onActivityPaused(activity)
                    tracker.onActivityStopped(activity)
                    assertEquals(0L, gauges["app.lifecycle.started_activities"])
                    assertEquals(1, events.count { it.endsWith("lifecycle.paused.count") })
                    assertEquals(1, events.count { it.endsWith("lifecycle.stopped.count") })
                } finally {
                    tracker.close()
                }
            }
        }
    }

    @Test
    fun stoppedTrackerCannotPublishWhileItsMainThreadUnregistrationIsQueued() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val state = RuntimeState()
        val inline = AtomicBoolean(true)
        val queue = ArrayDeque<() -> Unit>()
        val screenWrites = AtomicInteger()
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader, arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, _ ->
            if (method.name == "setScreen") screenWrites.incrementAndGet()
            when (method.returnType) {
                java.lang.Boolean.TYPE -> false
                java.lang.Long.TYPE -> 0L
                java.lang.Integer.TYPE -> 0
                String::class.java -> ""
                else -> null
            }
        }
        val service = RuntimeCollectorService(
            state, RuntimeCollectorCallbacks::class.java.cast(proxy)!!, { error("watcher disabled") },
            RuntimeMainThreadDispatcher(isMainThread = inline::get, postToMain = { queue.addLast(it); true }),
        )
        val application = ApplicationProvider.getApplicationContext<Application>()
        val directory = File(application.cacheDir, "art-old-tracker-${System.nanoTime()}")
        val config = JankHunterConfig.builder().fpsMonitorEnabled(false).jankStatsEnabled(false)
            .systemSamplerEnabled(false).mainLooperDispatchMonitorEnabled(false)
            .processExitInfoEnabled(false).objectWatcherEnabled(false).build()
        try {
            instrumentation.runOnMainSync { service.start(application, config, directory) }
            ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
                val oldTracker = checkNotNull(state.activityTracker)
                inline.set(false)
                service.stopProducers(100L)
                assertTrue("cleanup did not remain queued", queue.isNotEmpty())
                screenWrites.set(0)
                scenario.onActivity { oldTracker.onActivityResumed(it) }
                assertEquals("stopped tracker still published lifecycle context", 0, screenWrites.get())
            }
        } finally {
            inline.set(true)
            instrumentation.runOnMainSync {
                while (queue.isNotEmpty()) queue.removeFirst().invoke()
                service.stop()
                service.reset()
            }
            directory.deleteRecursively()
        }
    }

    @Test
    fun enablingRuntimeAfterActivityResumedStartsBothFrameSources() = withRuntime(initiallyEnabled = false) { graph ->
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity {
                assertTrue(graph.lifecycle.setRuntimeEnabled(true, "art-enable"))
                assertFrameObservation(graph)
            }
        }
    }

    @Test
    fun enablingInsideAnEarlierApplicationCallbackDoesNotMissTheCurrentResume() {
        val application = ApplicationProvider.getApplicationContext<Application>()
        var enable: () -> Unit = {}
        val earlierCallback = ResumeCallbacks { enable() }
        application.registerActivityLifecycleCallbacks(earlierCallback)
        try {
            withRuntime(initiallyEnabled = false) { graph ->
                enable = { assertTrue(graph.lifecycle.setRuntimeEnabled(true, "art-during-resume")) }
                ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
                    scenario.onActivity { assertFrameObservation(graph) }
                }
                enable = {}
            }
        } finally {
            application.unregisterActivityLifecycleCallbacks(earlierCallback)
        }
    }

    @Test
    fun reconfigurationRetainsAnAlreadyResumedWindow() = withRuntime(initiallyEnabled = true) { graph ->
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity {
                assertFrameObservation(graph)
                assertTrue(graph.lifecycle.reconfigure("art-reconfigure") { builder -> builder.fpsWindowMs(750L) })
                assertFrameObservation(graph)
            }
        }
    }

    @Test
    fun disableAndEnableWithoutLifecycleTransitionRestoresTheWindow() = withRuntime(initiallyEnabled = true) { graph ->
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity {
                assertFrameObservation(graph)
                assertTrue(graph.lifecycle.setRuntimeEnabled(false, "art-disable"))
                assertTrue(graph.lifecycle.setRuntimeEnabled(true, "art-enable"))
                assertFrameObservation(graph)
            }
        }
    }

    @Test
    fun permanentlyDisabledReinitializationClosesTheLifecycleObserver() = withRuntime(initiallyEnabled = false) { graph ->
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity { activity ->
                assertEquals(1, graph.state.activityObservation.snapshot().size)
                graph.lifecycle.init(activity.application, JankHunterConfig.builder().enabled(false).build())
                assertTrue("disabled SDK kept its old lifecycle observation", graph.state.activityObservation.snapshot().isEmpty())
            }
        }
    }

    @Test
    fun destroyedActivityIsNotRestoredAndNextActivityIsObservedNormally() = withRuntime(initiallyEnabled = false) { graph ->
        val application = ApplicationProvider.getApplicationContext<Application>()
        val trace = mutableListOf<String>()
        val destruction = ActivityDestructionBarrier()
        val startedField = ActivityTracker::class.java.getDeclaredField("startedActivities").apply { isAccessible = true }
        val recorder = LifecycleRecorder { activity, event ->
            val count = graph.state.activityTracker?.let(startedField::getInt)
            trace.add("$event:${System.identityHashCode(activity)}:$count")
            destruction.record(activity, event)
        }
        application.registerActivityLifecycleCallbacks(recorder)
        try {
            ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
                scenario.onActivity { assertEquals(1, graph.state.activityObservation.snapshot().size) }
            }
            destruction.awaitDestroyed()
            InstrumentationRegistry.getInstrumentation().runOnMainSync {
                assertTrue(graph.state.activityObservation.snapshot().isEmpty())
                assertTrue(graph.lifecycle.setRuntimeEnabled(true, "art-after-destroy"))
                val sources = frameSources(graph)
                assertFalse("destroyed Activity restored a frame window", sources.windowActive)
                assertFalse("destroyed Activity restored JankStats", sources.jankStatsActive)
            }
            ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
                scenario.onActivity {
                    val tracker = checkNotNull(graph.state.activityTracker)
                    assertFrameObservation(graph, "started=${startedField.getInt(tracker)}; trace=$trace; activity=${System.identityHashCode(it)}")
                }
            }
            destruction.awaitDestroyed()
            InstrumentationRegistry.getInstrumentation().runOnMainSync {
                assertTrue(graph.state.activityObservation.snapshot().isEmpty())
                val tracker = checkNotNull(graph.state.activityTracker)
                assertFalse(
                    "destroyed window remained active; started=${startedField.getInt(tracker)}; " +
                        "guardFailures=${RuntimeHookFailureTracker.count(RuntimeHookFailureReason.UNCLASSIFIED)}; trace=$trace",
                    frameSources(graph).windowActive,
                )
                assertFalse(frameSources(graph).jankStatsActive)
            }
        } finally {
            application.unregisterActivityLifecycleCallbacks(recorder)
        }
    }

    @Test
    fun fullShutdownReleasesObservedActivities() = withRuntime(initiallyEnabled = true) { graph ->
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity {
                assertEquals(1, graph.state.activityObservation.snapshot().size)
                graph.lifecycle.shutdown()
                assertTrue(graph.state.activityObservation.snapshot().isEmpty())
            }
        }
        ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
            scenario.onActivity { assertTrue(graph.state.activityObservation.snapshot().isEmpty()) }
        }
    }

    private fun assertFrameObservation(graph: RuntimeComponentGraph, trace: String = "") {
        assertTrue("runtime did not start: ${graph.lifecycle.diagnostics()}", graph.lifecycle.isStarted())
        val sources = frameSources(graph)
        assertTrue("resumed Activity lost its active frame window; $trace", sources.windowActive)
        assertTrue("resumed Activity lost JankStats tracking; $trace", sources.jankStatsActive)
    }

    private fun frameSources(graph: RuntimeComponentGraph): FrameSourceSelector {
        val monitor = checkNotNull(graph.state.fpsMonitor)
        val field = FpsMonitor::class.java.getDeclaredField("sourceSelector").apply { isAccessible = true }
        return field.get(monitor) as FrameSourceSelector
    }

    private fun withRuntime(initiallyEnabled: Boolean, action: (RuntimeComponentGraph) -> Unit) {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val application = ApplicationProvider.getApplicationContext<Application>()
        val directory = File(application.cacheDir, "art-resume-${System.nanoTime()}")
        val graph = RuntimeComponentGraph(
            nowMs = SystemClock::elapsedRealtime,
            nowUs = { SystemClock.elapsedRealtimeNanos() / 1_000L },
        )
        val config = JankHunterConfig.builder().runtimeEnabled(initiallyEnabled).logDirectory(directory)
            .fpsMonitorEnabled(true).jankStatsEnabled(true).systemSamplerEnabled(false)
            .mainLooperDispatchMonitorEnabled(false).processExitInfoEnabled(false).objectWatcherEnabled(false).build()
        try {
            // Documented initialization precedes Activity creation, including when collection is disabled.
            instrumentation.runOnMainSync { graph.lifecycle.init(application, config) }
            action(graph)
        } finally {
            instrumentation.runOnMainSync { graph.lifecycle.shutdown() }
            directory.deleteRecursively()
        }
    }

    private class ResumeCallbacks(private val onResume: () -> Unit) : Application.ActivityLifecycleCallbacks {
        override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = Unit
        override fun onActivityStarted(activity: Activity) = Unit
        override fun onActivityResumed(activity: Activity) = onResume()
        override fun onActivityPaused(activity: Activity) = Unit
        override fun onActivityStopped(activity: Activity) = Unit
        override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit
        override fun onActivityDestroyed(activity: Activity) = Unit
    }

    private class LifecycleRecorder(private val record: (Activity, String) -> Unit) : Application.ActivityLifecycleCallbacks {
        override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = record(activity, "created")
        override fun onActivityStarted(activity: Activity) = record(activity, "started")
        override fun onActivityResumed(activity: Activity) = record(activity, "resumed")
        override fun onActivityPaused(activity: Activity) = record(activity, "paused")
        override fun onActivityStopped(activity: Activity) = record(activity, "stopped")
        override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit
        override fun onActivityDestroyed(activity: Activity) = record(activity, "destroyed")
    }

    /** ActivityScenario can reach its terminal state before the app's destroy callbacks finish. */
    private class ActivityDestructionBarrier {
        private val lock = ReentrantLock()
        private val destroyed = lock.newCondition()
        private val live = java.util.Collections.newSetFromMap(java.util.IdentityHashMap<Activity, Boolean>())

        fun record(activity: Activity, event: String) = lock.withLock {
            if (event == "created") live.add(activity)
            if (event == "destroyed") {
                live.remove(activity)
                if (live.isEmpty()) destroyed.signalAll()
            }
        }

        fun awaitDestroyed() = lock.withLock {
            var remaining = TimeUnit.SECONDS.toNanos(3L)
            while (live.isNotEmpty() && remaining > 0L) remaining = destroyed.awaitNanos(remaining)
            assertTrue("Activity destroy callbacks did not finish", live.isEmpty())
        }
    }
}
