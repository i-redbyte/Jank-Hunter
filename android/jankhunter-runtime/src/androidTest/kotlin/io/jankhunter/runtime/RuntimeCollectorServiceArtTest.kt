package io.jankhunter.runtime

import android.app.Application
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import io.jankhunter.runtime.internal.system.RuntimeMainThreadDispatcher
import java.lang.reflect.Proxy
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class RuntimeCollectorServiceArtTest {
    @Test
    fun delayedActivityRegistrationCannotEscapeACompletedStop() {
        val state = RuntimeState()
        val queuedMainTasks = ArrayDeque<() -> Unit>()
        val executeInline = AtomicBoolean(false)
        val application = TrackingApplication()
        val service = RuntimeCollectorService(
            state = state,
            callbacks = noOpCallbacks(),
            bindRetentionWatcher = { error("Object watcher is disabled in this scenario") },
            mainThreadDispatcher = RuntimeMainThreadDispatcher(
                isMainThread = executeInline::get,
                postToMain = { task ->
                    queuedMainTasks.addLast(task)
                    true
                },
            ),
        )
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val directory = context.cacheDir.resolve("jankhunter-collector-${System.nanoTime()}")
        val config = JankHunterConfig.builder()
            .fpsMonitorEnabled(false)
            .jankStatsEnabled(false)
            .systemSamplerEnabled(false)
            .mainLooperDispatchMonitorEnabled(false)
            .processExitInfoEnabled(false)
            .objectWatcherEnabled(false)
            .build()

        try {
            service.start(application, config, directory)
            assertEquals(1, queuedMainTasks.size)

            executeInline.set(true)
            service.stop()
            queuedMainTasks.removeFirst().invoke()

            assertEquals(0, application.registrationCount)
        } finally {
            executeInline.set(true)
            service.stop()
            service.reset()
            directory.deleteRecursively()
        }
    }

    @Test
    fun rejectedMainThreadCleanupStillReleasesActivityTracker() {
        val state = RuntimeState()
        val rejectMainDispatch = AtomicBoolean(false)
        val application = TrackingApplication()
        val service = RuntimeCollectorService(
            state = state,
            callbacks = noOpCallbacks(),
            bindRetentionWatcher = { error("Object watcher is disabled in this scenario") },
            mainThreadDispatcher = RuntimeMainThreadDispatcher(
                isMainThread = { !rejectMainDispatch.get() },
                postToMain = { false },
            ),
        )
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val directory = context.cacheDir.resolve("jankhunter-collector-${System.nanoTime()}")
        val config = JankHunterConfig.builder()
            .fpsMonitorEnabled(false)
            .jankStatsEnabled(false)
            .systemSamplerEnabled(false)
            .mainLooperDispatchMonitorEnabled(false)
            .processExitInfoEnabled(false)
            .objectWatcherEnabled(false)
            .build()

        try {
            service.start(application, config, directory)
            assertEquals(1, application.registrationCount)

            rejectMainDispatch.set(true)
            service.stop()

            assertEquals(0, application.registrationCount)
        } finally {
            rejectMainDispatch.set(false)
            service.stop()
            service.reset()
            directory.deleteRecursively()
        }
    }

    @Test
    fun throwingActivityRegistrationDoesNotRetainApplicationOrTracker() {
        val state = RuntimeState()
        val application = ThrowingRegistrationApplication()
        val service = RuntimeCollectorService(
            state = state,
            callbacks = noOpCallbacks(),
            bindRetentionWatcher = { error("Object watcher is disabled in this scenario") },
            mainThreadDispatcher = RuntimeMainThreadDispatcher(isMainThread = { true }),
        )
        val context = ApplicationProvider.getApplicationContext<android.content.Context>()
        val directory = context.cacheDir.resolve("jankhunter-collector-${System.nanoTime()}")
        val config = JankHunterConfig.builder()
            .fpsMonitorEnabled(false)
            .jankStatsEnabled(false)
            .systemSamplerEnabled(false)
            .mainLooperDispatchMonitorEnabled(false)
            .processExitInfoEnabled(false)
            .objectWatcherEnabled(false)
            .build()

        try {
            service.start(application, config, directory)

            assertNull(state.application)
            assertNull(state.activityTracker)
            assertEquals("partial registration leaked its callback", 0, application.callbacks.size)
        } finally {
            service.stop()
            service.reset()
            directory.deleteRecursively()
        }
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

    private class TrackingApplication : Application() {
        private val callbacks = linkedSetOf<ActivityLifecycleCallbacks>()

        val registrationCount: Int
            get() = callbacks.size

        override fun registerActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            callbacks += callback
        }

        override fun unregisterActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            callbacks -= callback
        }
    }

    private class ThrowingRegistrationApplication : Application() {
        val callbacks = linkedSetOf<ActivityLifecycleCallbacks>()

        override fun registerActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            callbacks.add(callback)
            error("registration unavailable")
        }

        override fun unregisterActivityLifecycleCallbacks(callback: ActivityLifecycleCallbacks) {
            callbacks.remove(callback)
        }
    }
}
