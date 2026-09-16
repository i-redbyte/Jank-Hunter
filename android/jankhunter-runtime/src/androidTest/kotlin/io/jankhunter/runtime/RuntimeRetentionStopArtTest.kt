package io.jankhunter.runtime

import android.os.Looper
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class RuntimeRetentionStopArtTest {
    @Test
    fun mainThreadStopLeavesFinalDiagnosticsInBackgroundAndReturnsAtDeadline() {
        val now = AtomicLong()
        val dumpEntered = CountDownLatch(1)
        val releaseDump = CountDownLatch(1)
        val dumpThread = AtomicReference<Thread>()
        val gcThread = AtomicReference<Thread>()
        val scheduler = RuntimeMaintenanceScheduler()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L,
            clock = now::get,
            exactAdmission = true,
            forceGcBeforeReport = true,
            requestGc = { gcThread.set(Thread.currentThread()) },
            heapDumpReporter = { _, _, _, _, _ ->
                dumpThread.set(Thread.currentThread())
                dumpEntered.countDown()
                check(releaseDump.await(5L, TimeUnit.SECONDS))
            },
        )
        val retained = ByteArray(1_024)
        watcher.start(scheduler)
        try {
            watcher.watch(retained, "ArtRetainedOwner", null, null)
            now.set(2_000L)
            val result = AtomicReference<ObjectRetentionWatcher.StopResult>()
            val elapsedMs = AtomicLong()
            InstrumentationRegistry.getInstrumentation().runOnMainSync {
                val startMs = SystemClock.elapsedRealtime()
                result.set(watcher.stop(1_000L))
                elapsedMs.set(SystemClock.elapsedRealtime() - startMs)
            }
            assertEquals(ObjectRetentionWatcher.StopResult.TIMED_OUT, result.get())
            assertTrue("main stop exceeded its budget: ${elapsedMs.get()} ms", elapsedMs.get() < 1_500L)
            assertTrue(dumpEntered.await(1L, TimeUnit.SECONDS))
            assertNotSame(Looper.getMainLooper().thread, dumpThread.get())
            assertNotSame(Looper.getMainLooper().thread, gcThread.get())
            assertEquals(1_024, retained.size)
        } finally {
            releaseDump.countDown()
            watcher.stop(1_000L)
            scheduler.shutdown(1_000L)
        }
    }
}
