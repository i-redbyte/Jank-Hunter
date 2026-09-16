package io.jankhunter.runtime

import android.app.Activity
import android.app.Application
import android.content.ComponentName
import android.os.Debug
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.system.ActivityObservationStore
import io.jankhunter.runtime.internal.system.ActivityTracker
import io.jankhunter.runtime.internal.system.BoundedRegistry
import java.io.File
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.lang.reflect.Proxy
import java.util.concurrent.TimeUnit
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ActivityObservationArtTest {
    @Test
    fun telemetryFailureDuringStartCannotTransferItsCountToAnotherActivity() = InstrumentationRegistry.getInstrumentation().runOnMainSync {
        val gauges = mutableMapOf<String, Long>()
        var failFirstVisibility = true
        val tracker = ActivityTracker(callbacks(mutableListOf(), gauges) { name ->
            if (name == "app.lifecycle.ui_visible.count" && failFirstVisibility) {
                failFirstVisibility = false
                error("metric unavailable")
            }
        })
        try {
            val first = NamedActivity()
            tracker.onActivityStarted(first)
            tracker.onActivityStarted(NamedActivity())
            tracker.onActivityStopped(first)
            assertEquals(1L, gauges["app.lifecycle.started_activities"])
        } finally {
            tracker.close()
        }
    }

    @Test
    fun failingDestroyedMetricCannotRetainTheDestroyedActivity() = InstrumentationRegistry.getInstrumentation().runOnMainSync {
        val tracker = ActivityTracker(callbacks(mutableListOf()) { name ->
            if (name.endsWith(".lifecycle.destroyed.count")) error("metric unavailable")
        })
        try {
            val activity = NamedActivity()
            tracker.onActivityStarted(activity)
            tracker.onActivityDestroyed(activity)
            val field = ActivityTracker::class.java.getDeclaredField("activityStates").apply { isAccessible = true }
            assertEquals("failed metric retained destroyed Activity", 0, (field.get(tracker) as BoundedRegistry<*, *>).size())
        } finally {
            tracker.close()
        }
    }

    @Test
    fun lateStopFromAnUnobservedActivityCannotCloseTheCurrentWindow() = InstrumentationRegistry.getInstrumentation().runOnMainSync {
        val events = mutableListOf<String>()
        val gauges = mutableMapOf<String, Long>()
        val tracker = ActivityTracker(callbacks(events, gauges))
        try {
            val current = NamedActivity()
            tracker.onActivityStarted(current)
            val beforeOrphan = events.count { it == "open" }
            tracker.onActivityStopped(NamedActivity())
            assertEquals("orphan stop consumed the current Activity", 1L, gauges["app.lifecycle.started_activities"])
            assertEquals("orphan stop invented a screen-open operation", beforeOrphan, events.count { it == "open" })
            tracker.onActivityStopped(current)
            assertEquals(0L, gauges["app.lifecycle.started_activities"])
        } finally {
            tracker.close()
        }
    }

    @Test
    fun allStoppedActivitiesReleaseTheWindowAfterRegistryOverflow() = InstrumentationRegistry.getInstrumentation().runOnMainSync {
        val gauges = mutableMapOf<String, Long>()
        val tracker = ActivityTracker(callbacks(mutableListOf(), gauges))
        val activities = List(80) { NamedActivity() }
        try {
            activities.forEach(tracker::onActivityStarted)
            activities.forEach(tracker::onActivityStopped)
            assertEquals(0L, gauges["app.lifecycle.started_activities"])
        } finally {
            tracker.close()
        }
    }

    @Test
    fun destroyedActivityCannotLeaveItsStartedCountBehind() = InstrumentationRegistry.getInstrumentation().runOnMainSync {
        val gauges = mutableMapOf<String, Long>()
        val tracker = ActivityTracker(callbacks(mutableListOf(), gauges))
        try {
            val activity = NamedActivity()
            tracker.onActivityStarted(activity)
            tracker.onActivityDestroyed(activity)
            assertEquals(0L, gauges["app.lifecycle.started_activities"])
        } finally {
            tracker.close()
        }
    }

    @Test
    fun currentTrackerReceivesEachLifecycleEventOnceAndDetachStopsDelivery() {
        val app = ObservedApplication()
        val store = ActivityObservationStore()
        val events = mutableListOf<String>()
        val tracker = ActivityTracker(callbacks(events))
        try {
            store.observe(app)
            assertTrue(store.attach(tracker))
            assertEquals(1, app.registrations)
            ActivityScenario.launch(JankStatsProbeActivity::class.java).use { scenario ->
                scenario.onActivity { activity ->
                    val callback = checkNotNull(app.callback)
                    callback.onActivityCreated(activity, null)
                    callback.onActivityStarted(activity)
                    callback.onActivityResumed(activity)
                    callback.onActivityPaused(activity)
                    callback.onActivityStopped(activity)
                    callback.onActivityDestroyed(activity)
                    for (event in listOf("created", "started", "resumed", "paused", "stopped", "destroyed")) {
                        assertEquals("duplicate or missing $event", 1, events.count { it.endsWith(".lifecycle.$event.count") })
                    }
                    val beforeDetach = events.size
                    store.detach(tracker)
                    callback.onActivityResumed(activity)
                    assertEquals(beforeDetach, events.size)
                    tracker.close()
                }
            }
        } finally {
            store.close()
        }
    }

    @Test
    fun detachedTrackerIsCollectibleWhileObserverRemainsRegistered() {
        val app = ObservedApplication()
        val store = ActivityObservationStore()
        val queue = ReferenceQueue<ActivityTracker>()
        try {
            store.observe(app)
            val weak = detachedTracker(store, queue)
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(2L)
            while (weak.get() != null && System.nanoTime() < deadline) {
                System.gc()
                queue.remove(25L)
            }
            assertNull("observer retained a detached collector", weak.get())
            assertEquals(1, app.registrations)
        } finally {
            store.close()
        }
    }

    @Test
    fun measureObservationCallbacks() {
        val arguments = InstrumentationRegistry.getArguments()
        assumeTrue(arguments.getString("jankhunterActivityBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val output = File(instrumentation.context.filesDir, "jankhunter-activity-$label.jsonl")
        output.writeText("")
        for (cardinality in intArrayOf(1, 64)) {
            instrumentation.runOnMainSync {
                val app = ObservedApplication()
                val store = ActivityObservationStore()
                store.observe(app)
                try {
                    val activities = Array(cardinality) { Activity() }
                    val callback = checkNotNull(app.callback)
                    activities.forEach(callback::onActivityStarted)
                    repeat(WARMUP) { cycle(callback, activities[it % cardinality]) }
                    val samples = LongArray(EVENTS)
                    val allocated = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                    val cpu = Debug.threadCpuTimeNanos()
                    val wall = System.nanoTime()
                    repeat(EVENTS) { index ->
                        val started = System.nanoTime()
                        cycle(callback, activities[index % cardinality])
                        samples[index] = System.nanoTime() - started
                    }
                    val wallNs = System.nanoTime() - wall
                    val cpuNs = Debug.threadCpuTimeNanos() - cpu
                    val allocatedBytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - allocated
                    samples.sort()
                    val snapshot = store.snapshot()
                    assertEquals(cardinality, snapshot.size)
                    assertTrue(snapshot.none { it.resumed })
                    assertEquals(0L, store.takeCapacityLoss())
                    output.appendText(JSONObject().put("label", label).put("cardinality", cardinality)
                        .put("cycles", EVENTS).put("wall_ns", wallNs).put("thread_cpu_ns", cpuNs)
                        .put("allocated_bytes", allocatedBytes).put("p50_ns", samples[EVENTS / 2])
                        .put("p95_ns", samples[EVENTS * 95 / 100]).put("p99_ns", samples[EVENTS * 99 / 100])
                        .toString() + "\n")
                    if (arguments.getString("jankhunterAssertNoCallbackAllocation") == "true") {
                        assertTrue("lifecycle observation allocated $allocatedBytes bytes", allocatedBytes <= EVENTS.toLong())
                    }
                } finally {
                    store.close()
                }
            }
        }
    }

    private fun detachedTracker(store: ActivityObservationStore, queue: ReferenceQueue<ActivityTracker>): WeakReference<ActivityTracker> {
        val tracker = ActivityTracker(callbacks(mutableListOf()))
        assertTrue(store.attach(tracker))
        store.detach(tracker)
        return WeakReference(tracker, queue)
    }

    private fun cycle(callback: Application.ActivityLifecycleCallbacks, activity: Activity) {
        callback.onActivityStarted(activity)
        callback.onActivityResumed(activity)
        callback.onActivityPaused(activity)
    }

    private fun callbacks(
        events: MutableList<String>,
        gauges: MutableMap<String, Long> = mutableMapOf(),
        beforeCounter: (String) -> Unit = {},
    ): RuntimeCollectorCallbacks {
        val proxy = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader, arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, args ->
            when (method.name) {
                "recordCounter" -> {
                    val name = args!![0] as String
                    beforeCounter(name)
                    events.add(name)
                }
                "startScreenOpenOperation" -> events.add("open")
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
        return checkNotNull(RuntimeCollectorCallbacks::class.java.cast(proxy))
    }

    private class NamedActivity : Activity() {
        override fun getComponentName(): ComponentName = ComponentName("io.jankhunter.test", "io.jankhunter.test.Activity")
    }

    private class ObservedApplication : Application() {
        var callback: ActivityLifecycleCallbacks? = null
        var registrations = 0
        override fun registerActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            check(this.callback == null)
            this.callback = callback
            registrations++
        }
        override fun unregisterActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            if (this.callback === callback) this.callback = null
        }
    }

    private companion object {
        const val WARMUP = 50_000
        const val EVENTS = 50_000
    }
}
