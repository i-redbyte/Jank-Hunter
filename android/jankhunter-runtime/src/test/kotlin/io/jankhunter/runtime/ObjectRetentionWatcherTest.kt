package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import java.util.concurrent.atomic.AtomicBoolean
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
            retainedDelayMs = RETAINED_DELAY_MS,
            forceGcBeforeReport = true,
            clock = { now },
            requestGc = { gcRequests++ },
            reporter = { className, ownerHint, context, ageMs, count ->
                reports += Report(className, ownerHint, context, ageMs, count)
            },
        )
        val first = Any()
        val second = Any()
        enableManualWatch(watcher)
        try {
            watcher.watch(first, "com.example.LeakyOwner", "com.example.Holder", null)
            watcher.watch(second, "com.example.LeakyOwner", "com.example.Holder", null)

            now = RETAINED_DELAY_MS
            watcher.checkRetained()
            assertEquals(1, gcRequests)
            assertTrue(reports.isEmpty())

            now = RETAINED_DELAY_MS + 600L
            watcher.checkRetained()
            assertEquals(listOf(Report("com.example.LeakyOwner", "com.example.Holder", null, now, 2L)), reports)
        } finally {
            watcher.stop()
        }
    }

    @Test
    fun compactsReportedReferencesInBatch() {
        var now = 0L
        val reports = mutableListOf<Report>()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = RETAINED_DELAY_MS,
            clock = { now },
            reporter = { className, ownerHint, context, ageMs, count ->
                reports += Report(className, ownerHint, context, ageMs, count)
            },
        )
        val retained = List(1_000) { Any() }
        enableManualWatch(watcher)
        try {
            retained.forEach { instance ->
                watcher.watch(instance, "com.example.BusyScreen", "com.example.Holder", null)
            }

            now = RETAINED_DELAY_MS
            watcher.checkRetained()
            assertTrue(reports.isEmpty())

            now = RETAINED_DELAY_MS + 500L
            watcher.checkRetained()
            assertEquals(listOf(Report("com.example.BusyScreen", "com.example.Holder", null, now, 1_000L)), reports)

            now = RETAINED_DELAY_MS + 1_000L
            watcher.checkRetained()
            assertEquals(1, reports.size)
        } finally {
            watcher.stop()
        }
    }

    @Test
    fun keepsWatchTimeContextForRetainedReport() {
        var now = 0L
        val reports = mutableListOf<Report>()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = RETAINED_DELAY_MS,
            clock = { now },
            reporter = { className, ownerHint, context, ageMs, count ->
                reports += Report(className, ownerHint, context, ageMs, count)
            },
        )
        val retained = Any()
        val context = JankHunterContext(
            screen = "LeakDemoScreen",
            owner = "sample.memory_leak.listener_registry",
            flow = "sample.memory_leak.demo",
            step = "listener_callback",
        )
        enableManualWatch(watcher)
        try {
            watcher.watch(retained, "com.example.ListenerLeak", "sample.memory_leak.listener_registry", context)

            now = RETAINED_DELAY_MS
            watcher.checkRetained()
            now = RETAINED_DELAY_MS + 500L
            watcher.checkRetained()

            assertEquals(
                listOf(Report("com.example.ListenerLeak", "sample.memory_leak.listener_registry", context, now, 1L)),
                reports,
            )
        } finally {
            watcher.stop()
        }
    }

    private data class Report(
        val className: String?,
        val ownerHint: String?,
        val context: JankHunterContext?,
        val ageMs: Long,
        val count: Long,
    )

    private fun enableManualWatch(watcher: ObjectRetentionWatcher) {
        val runningField = ObjectRetentionWatcher::class.java.getDeclaredField("running").apply {
            isAccessible = true
        }
        (runningField.get(watcher) as AtomicBoolean).set(true)
    }

    private companion object {
        const val RETAINED_DELAY_MS = 20_000L
    }
}
