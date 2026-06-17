package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ObjectRetentionWatcherTest {
    @Test
    fun groupsRetainedObjectsAfterRepeatedCheckAndGcRequest() {
        var now = 0L
        var gcRequests = 0
        val reports = mutableListOf<Report>()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L,
            forceGcBeforeReport = true,
            clock = { now },
            requestGc = { gcRequests++ },
            reporter = { className, ownerHint, ageMs, count ->
                reports += Report(className, ownerHint, ageMs, count)
            },
        )
        val first = Any()
        val second = Any()
        watcher.watchForTest(first, "com.example.LeakyOwner", "com.example.Holder")
        watcher.watchForTest(second, "com.example.LeakyOwner", "com.example.Holder")

        now = 1_000L
        watcher.checkRetained()
        assertEquals(1, gcRequests)
        assertTrue(reports.isEmpty())

        now = 1_600L
        watcher.checkRetained()
        assertEquals(listOf(Report("com.example.LeakyOwner", "com.example.Holder", 1_600L, 2L)), reports)
    }

    @Test
    fun compactsReportedReferencesInBatch() {
        var now = 0L
        val reports = mutableListOf<Report>()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = 1_000L,
            clock = { now },
            reporter = { className, ownerHint, ageMs, count ->
                reports += Report(className, ownerHint, ageMs, count)
            },
        )
        val retained = List(1_000) { Any() }
        retained.forEach { instance ->
            watcher.watchForTest(instance, "com.example.BusyScreen", "com.example.Holder")
        }
        assertEquals(1_000, watcher.watchedCountForTest())

        now = 1_000L
        watcher.checkRetained()
        assertEquals(1_000, watcher.watchedCountForTest())
        assertTrue(reports.isEmpty())

        now = 1_500L
        watcher.checkRetained()
        assertEquals(0, watcher.watchedCountForTest())
        assertEquals(listOf(Report("com.example.BusyScreen", "com.example.Holder", 1_500L, 1_000L)), reports)
    }

    private data class Report(
        val className: String?,
        val ownerHint: String?,
        val ageMs: Long,
        val count: Long,
    )
}
