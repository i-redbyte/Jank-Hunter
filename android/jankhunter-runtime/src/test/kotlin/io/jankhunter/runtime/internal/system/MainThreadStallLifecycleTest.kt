package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.MainThreadStallState
import io.jankhunter.runtime.RuntimeCollectorCallbacks
import java.lang.reflect.Proxy
import java.util.concurrent.CountDownLatch
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MainThreadStallLifecycleTest {
    @Test
    fun lateOldMonitorCannotCompleteOrClearANewGeneration() {
        val fixture = Fixture()
        val oldRecording = CountDownLatch(1)
        val releaseOld = CountDownLatch(1)
        fixture.beforeRecord = { id, state ->
            if (id == 1L && state == MainThreadStallState.ONGOING) {
                oldRecording.countDown()
                var released = false
                while (!released) {
                    try {
                        released = releaseOld.await(2L, TimeUnit.SECONDS)
                        check(released) { "Test did not release the old reporter" }
                    } catch (_: InterruptedException) {
                        // Model an already admitted I/O operation that does not obey interruption.
                    }
                }
            }
        }
        try {
            fixture.watchdog.start()
            fixture.main.now.set(200L)
            assertTrue(oldRecording.await(1L, TimeUnit.SECONDS))
            assertFalse(fixture.watchdog.stop(1L))
            fixture.watchdog.start()
            fixture.main.now.set(400L)
            assertTrue(fixture.recorded.await(1L, TimeUnit.SECONDS))
            assertEquals(listOf(2L), fixture.events.map { it.id })
            releaseOld.countDown()
            assertTrue(fixture.terminated.await(1L, TimeUnit.SECONDS))
            fixture.main.now.set(500L)
            fixture.main.runHeartbeat()
            assertTrue(fixture.newRecovery.await(1L, TimeUnit.SECONDS))
            assertEquals(listOf(MainThreadStallState.ONGOING, MainThreadStallState.INTERRUPTED),
                fixture.events.filter { it.id == 1L }.map { it.state })
            assertEquals(listOf(MainThreadStallState.ONGOING, MainThreadStallState.RECOVERED),
                fixture.events.filter { it.id == 2L }.map { it.state })
        } finally {
            releaseOld.countDown()
            assertTrue(fixture.watchdog.stop(1_000L))
        }
    }

    @Test
    fun elapsedTimeSpentAsleepDoesNotBecomeAMainThreadStall() {
        val fixture = Fixture()
        try {
            fixture.watchdog.start()
            fixture.main.elapsedOffset.set(1_000_000L)
            val polled = CountDownLatch(3)
            fixture.main.polls = polled
            assertTrue(polled.await(1L, TimeUnit.SECONDS))
            assertEquals(1_000_000L, fixture.main.elapsedRealtime())
            assertEquals(0L, fixture.main.now.get())
            assertEquals("deep sleep was reported as a main-thread stall", 0, fixture.recordCount.get())
        } finally {
            fixture.watchdog.stop()
            assertTrue(fixture.terminated.await(1L, TimeUnit.SECONDS))
        }
    }

    @Test
    fun unrecoveredStallIsRecordedWhileMainThreadRemainsBlocked() = withWatchdog { fixture ->
        assertTrue("stall stack was not captured", fixture.captured.await(1L, TimeUnit.SECONDS))
        assertTrue("captured stall disappeared until recovery", fixture.recorded.await(500L, TimeUnit.MILLISECONDS))
        assertEquals(0, fixture.main.heartbeatExecutions.get())
    }

    @Test
    fun stoppingAnUnrecoveredStallPublishesItsTerminalEvidence() = withWatchdog { fixture ->
        assertTrue(fixture.captured.await(1L, TimeUnit.SECONDS))
        fixture.watchdog.stop()
        assertTrue(fixture.terminated.await(1L, TimeUnit.SECONDS))
        assertEquals("expected initial and interrupted evidence for the same stall", 2, fixture.recordCount.get())
        assertEquals(listOf(MainThreadStallState.ONGOING, MainThreadStallState.INTERRUPTED), fixture.events.map { it.state })
        assertEquals(listOf(1L, 1L), fixture.events.map { it.id })
    }

    @Test
    fun recoveryCompletesTheSameIncidentAndUsesTheFirstHeartbeatTime() = withWatchdog { fixture ->
        assertTrue(fixture.recorded.await(1L, TimeUnit.SECONDS))
        fixture.main.now.set(350L)
        fixture.main.runHeartbeat()
        fixture.main.now.set(370L)
        fixture.main.runHeartbeat()
        assertTrue(fixture.terminalRecorded.await(1L, TimeUnit.SECONDS))
        assertEquals(listOf(MainThreadStallState.ONGOING, MainThreadStallState.RECOVERED), fixture.events.map { it.state })
        assertEquals(listOf(1L, 1L), fixture.events.map { it.id })
        assertEquals(350L, fixture.events.last().durationMs)
    }

    @Test
    fun repeatedPollsDoNotWriteUnboundedOngoingUpdates() = withWatchdog { fixture ->
        assertTrue(fixture.recorded.await(1L, TimeUnit.SECONDS))
        val polled = CountDownLatch(3)
        fixture.main.polls = polled
        assertTrue(polled.await(1L, TimeUnit.SECONDS))
        assertEquals(1, fixture.recordCount.get())
    }

    private fun withWatchdog(block: (Fixture) -> Unit) {
        val fixture = Fixture()
        try {
            fixture.watchdog.start()
            fixture.main.now.set(200L)
            block(fixture)
        } finally {
            fixture.watchdog.stop()
            assertTrue(fixture.terminated.await(1L, TimeUnit.SECONDS))
        }
    }

    private class Fixture {
        val captured = CountDownLatch(1)
        val recorded = CountDownLatch(1)
        val terminated = CountDownLatch(1)
        val terminalRecorded = CountDownLatch(1)
        val newRecovery = CountDownLatch(1)
        var beforeRecord: ((Long, MainThreadStallState) -> Unit)? = null
        val recordCount = AtomicInteger()
        val events = CopyOnWriteArrayList<Observation>()
        private val ids = AtomicLong()
        val main = ControlledMainThread()
        private val callbacks = Proxy.newProxyInstance(
            RuntimeCollectorCallbacks::class.java.classLoader,
            arrayOf(RuntimeCollectorCallbacks::class.java),
        ) { _, method, args ->
            when (method.name) {
                "nextMainThreadStallId" -> ids.incrementAndGet()
                "captureMainThreadStallContext" -> {
                    captured.countDown()
                    JankHunterContextSnapshot("screen", "owner")
                }
                "recordMainThreadStall" -> {
                    val values = requireNotNull(args)
                    val state = values[4] as MainThreadStallState
                    val id = values[3] as Long
                    beforeRecord?.invoke(id, state)
                    events.add(Observation(values[3] as Long, values[2] as Long, state))
                    recordCount.incrementAndGet()
                    recorded.countDown()
                    if (state != MainThreadStallState.ONGOING) terminalRecorded.countDown()
                    if (id == 2L && state == MainThreadStallState.RECOVERED) newRecovery.countDown()
                    null
                }
                else -> when (method.returnType) {
                    java.lang.Boolean.TYPE -> false
                    java.lang.Integer.TYPE -> 0
                    java.lang.Long.TYPE -> 0L
                    String::class.java -> ""
                    else -> null
                }
            }
        } as RuntimeCollectorCallbacks
        val watchdog = MainThreadWatchdog(100L, callbacks, main) { runnable, name ->
            Thread({
                try {
                    runnable.run()
                } finally {
                    terminated.countDown()
                }
            }, name)
        }
    }

    private class ControlledMainThread : WatchdogMainThread {
        val now = AtomicLong()
        val elapsedOffset = AtomicLong()
        val heartbeatExecutions = AtomicInteger()
        @Volatile var polls: CountDownLatch? = null
        @Volatile private var heartbeat: Runnable? = null
        fun runHeartbeat() {
            heartbeatExecutions.incrementAndGet()
            checkNotNull(heartbeat).run()
        }
        fun elapsedRealtime(): Long {
            polls?.countDown()
            return now.get() + elapsedOffset.get()
        }
        override fun uptimeMillis(): Long {
            polls?.countDown()
            return now.get()
        }
        override fun post(task: Runnable): Boolean {
            heartbeat = task
            return true
        }
        override fun postDelayed(task: Runnable, delayMs: Long): Boolean = true
        override fun removeCallbacks(task: Runnable) = Unit
        override fun stackTrace(): Array<StackTraceElement> = arrayOf(
            StackTraceElement("example.Screen", "blocked", "Screen.kt", 42),
        )
    }

    private data class Observation(val id: Long, val durationMs: Long, val state: MainThreadStallState)
}
