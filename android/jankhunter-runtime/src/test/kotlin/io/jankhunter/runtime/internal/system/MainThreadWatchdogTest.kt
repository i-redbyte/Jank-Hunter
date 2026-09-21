package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeCollectorCallbacks
import java.lang.reflect.Proxy
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class MainThreadWatchdogTest {
    @Test
    fun throwingThreadFactoryReleasesHeartbeatAndAllowsRetry() {
        val mainThread = FakeWatchdogMainThread(acceptPost = true)
        var failFactory = true
        val watchdog = MainThreadWatchdog(100L, noOpCallbacks(), mainThread) { runnable, name ->
            if (failFactory) error("thread allocation failed")
            Thread(runnable, name)
        }
        try {
            org.junit.Assert.assertThrows(IllegalStateException::class.java) { watchdog.start() }
            assertSame(mainThread.lastPosted, mainThread.lastRemoved)
            failFactory = false
            watchdog.start()
            assertEquals(2, mainThread.postCount)
        } finally {
            watchdog.stop()
        }
    }

    @Test
    fun stallEvidenceKeepsMaterialAndApplicationCallerWithoutGuessingProvenance() {
        val evidence = MainThreadStallEvidence(maxSamples = 4)

        evidence.addSample(
            arrayOf(
                StackTraceElement(
                    "com.google.android.material.appbar.AppBarLayout",
                    "<init>",
                    "AppBarLayout.java",
                    303,
                ),
                StackTraceElement("android.view.LayoutInflater", "createView", "LayoutInflater.java", 700),
                StackTraceElement("ru.mail.im.chat.ChatFragment", "onCreateView", "ChatFragment.kt", 121),
            ),
        )

        assertTrue(evidence.stackHint.contains("com.google.android.material.appbar.AppBarLayout.<init>"))
        assertTrue(evidence.stackHint.contains("android.view.LayoutInflater.createView"))
        assertTrue(evidence.stackHint.contains("ru.mail.im.chat.ChatFragment.onCreateView"))

    }

    @Test
    fun rejectedInitialHeartbeatDoesNotLeaveWatchdogRunning() {
        val mainThread = FakeWatchdogMainThread(acceptPost = false)
        val watchdog = MainThreadWatchdog(100L, noOpCallbacks(), mainThread)

        try {
            watchdog.start()
            mainThread.acceptPost = true
            watchdog.start()

            assertEquals(2, mainThread.postCount)
        } finally {
            watchdog.stop()
        }
    }

    @Test
    fun throwingInitialClockReadDoesNotLeaveWatchdogRunning() {
        val mainThread = FakeWatchdogMainThread(acceptPost = true, throwOnUptimeMillis = true)
        val watchdog = MainThreadWatchdog(100L, noOpCallbacks(), mainThread)

        try {
            watchdog.start()
            mainThread.throwOnUptimeMillis = false
            watchdog.start()

            assertEquals(2, mainThread.uptimeMillisCount)
            assertEquals(1, mainThread.postCount)
        } finally {
            watchdog.stop()
        }
    }

    @Test
    fun rejectedDelayedHeartbeatStopsCurrentGeneration() {
        val mainThread = FakeWatchdogMainThread(acceptPost = true, acceptDelayedPost = false)
        val watchdog = MainThreadWatchdog(100L, noOpCallbacks(), mainThread)

        try {
            watchdog.start()
            checkNotNull(mainThread.lastPosted).run()
            watchdog.start()

            assertEquals(2, mainThread.postCount)
        } finally {
            watchdog.stop()
        }
    }

    @Test
    fun stopRemovesPendingHeartbeatFromMainQueue() {
        val mainThread = FakeWatchdogMainThread(acceptPost = true)
        val watchdog = MainThreadWatchdog(100L, noOpCallbacks(), mainThread)

        watchdog.start()
        val heartbeat = checkNotNull(mainThread.lastPosted)
        watchdog.stop()

        assertSame(heartbeat, mainThread.lastRemoved)
    }

    @Test
    fun fatalMonitorFailureReleasesGenerationAndPendingHeartbeat() {
        val terminated = CountDownLatch(1)
        val mainThread = FakeWatchdogMainThread(
            acceptPost = true,
            fatalOnUptimeMillisCall = 2,
        )
        val watchdog = MainThreadWatchdog(100L, noOpCallbacks(), mainThread) { runnable, name ->
            Thread(runnable, name).apply {
                uncaughtExceptionHandler = Thread.UncaughtExceptionHandler { _, _ -> terminated.countDown() }
            }
        }

        try {
            watchdog.start()
            assertTrue(terminated.await(1L, TimeUnit.SECONDS))
            val firstHeartbeat = checkNotNull(mainThread.lastRemoved)

            mainThread.fatalOnUptimeMillisCall = null
            watchdog.start()

            assertEquals(2, mainThread.postCount)
            assertSame(firstHeartbeat, mainThread.lastRemoved)
        } finally {
            watchdog.stop()
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

    private class FakeWatchdogMainThread(
        var acceptPost: Boolean,
        private val acceptDelayedPost: Boolean = true,
        var throwOnUptimeMillis: Boolean = false,
        var fatalOnUptimeMillisCall: Int? = null,
    ) : WatchdogMainThread {
        var postCount = 0
        var uptimeMillisCount = 0
        var lastPosted: Runnable? = null
        var lastRemoved: Runnable? = null

        override fun uptimeMillis(): Long {
            uptimeMillisCount++
            if (fatalOnUptimeMillisCall == uptimeMillisCount) throw FatalWatchdogError()
            if (throwOnUptimeMillis) error("clock unavailable")
            return 100L
        }

        override fun post(task: Runnable): Boolean {
            postCount++
            if (acceptPost) lastPosted = task
            return acceptPost
        }

        override fun postDelayed(task: Runnable, delayMs: Long): Boolean = acceptDelayedPost

        override fun removeCallbacks(task: Runnable) {
            lastRemoved = task
        }

        override fun stackTrace(): Array<StackTraceElement> = emptyArray()
    }

    private class FatalWatchdogError : VirtualMachineError()
}
