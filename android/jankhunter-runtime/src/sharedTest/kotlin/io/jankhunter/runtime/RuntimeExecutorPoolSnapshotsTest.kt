package io.jankhunter.runtime

import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class RuntimeExecutorPoolSnapshotsTest {
    @Test
    fun nativeGettersOnlyRunInTheDiagnosticLaneAndRequestsAreCoalesced() {
        var reads = 0
        var now = 100L
        val jobs = ArrayList<() -> Unit>()
        val pool = object : ThreadPoolExecutor(0, 1, 1, TimeUnit.SECONDS, LinkedBlockingQueue()) {
            override fun getActiveCount(): Int { reads++; return 3 }
            override fun getPoolSize(): Int { reads++; return 5 }
            override fun getCompletedTaskCount(): Long { reads++; return 7L }
        }
        try {
            val snapshots = RuntimeExecutorPoolSnapshots({ now }, { jobs.add(it); true })
            repeat(1000) { assertNull(snapshots.getOrRequest(pool)) }
            assertEquals(0, reads)
            assertEquals(1, jobs.size)
            jobs.removeAt(0).invoke()
            val snapshot = checkNotNull(snapshots.getOrRequest(pool))
            assertEquals(3, snapshot.active)
            assertEquals(5, snapshot.size)
            assertEquals(7L, snapshot.completed)
            assertEquals(100L, snapshot.atMs)
            assertEquals(3, reads)
            now = 1100
            assertNotNull(snapshots.getOrRequest(pool))
            assertEquals(1, jobs.size)
            now = 2201
            assertNull(snapshots.getOrRequest(pool))
            assertEquals("One pending sample per pool", 1, jobs.size)
        } finally { pool.shutdownNow() }
    }

    @Test
    fun aRejectedDiagnosticRequestCanRetryWithoutAnInlineFallback() {
        var now = 0L
        var attempts = 0
        val pool = ThreadPoolExecutor(0, 1, 1, TimeUnit.SECONDS, LinkedBlockingQueue())
        try {
            val snapshots = RuntimeExecutorPoolSnapshots({ now }, { attempts++; false }, capacity = 1)
            assertNull(snapshots.getOrRequest(pool))
            repeat(100) { snapshots.getOrRequest(pool) }
            assertEquals(1, attempts)
            now = 1000
            assertNull(snapshots.getOrRequest(pool))
            assertEquals(2, attempts)
        } finally { pool.shutdownNow() }
    }

    @Test
    fun aSlowNativeSampleDoesNotMakeOldReadingsLookFresh() {
        var now = 100L
        val jobs = ArrayList<() -> Unit>()
        val pool = object : ThreadPoolExecutor(0, 1, 1, TimeUnit.SECONDS, LinkedBlockingQueue()) {
            override fun getActiveCount(): Int = 1
            override fun getPoolSize(): Int { now = 2201L; return 2 }
        }
        try {
            val snapshots = RuntimeExecutorPoolSnapshots({ now }, { jobs.add(it); true })
            assertNull(snapshots.getOrRequest(pool))
            jobs.removeAt(0).invoke()
            assertNull("The earliest reading is already older than the snapshot TTL", snapshots.getOrRequest(pool))
        } finally { pool.shutdownNow() }
    }
}
