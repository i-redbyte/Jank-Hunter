package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import io.jankhunter.runtime.internal.system.RetentionEvidence
import java.util.concurrent.atomic.AtomicBoolean
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ObjectRetentionWatcherTest {
    @Test
    fun retainedHolderFallsBackToClassNameWhenHolderIsMissing() {
        assertEquals("com.example.Owner", JankHunter.effectiveRetainedHolder("com.example.LeakyActivity", "com.example.Owner"))
        assertEquals("com.example.LeakyActivity", JankHunter.effectiveRetainedHolder("com.example.LeakyActivity", null))
        assertEquals("com.example.LeakyActivity", JankHunter.effectiveRetainedHolder("com.example.LeakyActivity", "unknown"))
    }

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
            reporter = { className, ownerHint, context, ageMs, count, evidence ->
                reports += Report(className, ownerHint, context, ageMs, count, evidence)
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
            assertEquals(
                listOf(
                    Report(
                        "com.example.LeakyOwner",
                        "com.example.Holder",
                        null,
                        now,
                        2L,
                        RetentionEvidence.AFTER_EXPLICIT_GC,
                    ),
                ),
                reports,
            )
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
            reporter = { className, ownerHint, context, ageMs, count, evidence ->
                reports += Report(className, ownerHint, context, ageMs, count, evidence)
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
            assertEquals(
                listOf(
                    Report(
                        "com.example.BusyScreen",
                        "com.example.Holder",
                        null,
                        RETAINED_DELAY_MS,
                        1_000L,
                        RetentionEvidence.TIME_ONLY,
                    ),
                ),
                reports,
            )

            now = RETAINED_DELAY_MS + 1_000L
            watcher.checkRetained()
            assertEquals(1, reports.size)
        } finally {
            watcher.stop()
        }
    }

    @Test
    fun keepsWeakWatchUntilHeapDumpAgeWithoutDuplicatingRetainedReport() {
        var now = 0L
        val reports = mutableListOf<Report>()
        val heapDumps = mutableListOf<Report>()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = RETAINED_DELAY_MS,
            clock = { now },
            reporter = { className, ownerHint, context, ageMs, count, evidence ->
                reports += Report(className, ownerHint, context, ageMs, count, evidence)
            },
            heapDumpMinRetainedAgeMs = HEAP_DUMP_AGE_MS,
            heapDumpReporter = { className, ownerHint, context, ageMs, count, evidence ->
                heapDumps += Report(className, ownerHint, context, ageMs, count, evidence)
            },
        )
        val retained = Any()
        enableManualWatch(watcher)
        try {
            watcher.watch(retained, "com.example.LeakyScreen", "com.example.Holder", null)

            now = RETAINED_DELAY_MS
            watcher.checkRetained()

            assertEquals(1, reports.size)
            assertTrue(heapDumps.isEmpty())

            now = HEAP_DUMP_AGE_MS - 1L
            watcher.checkRetained()
            assertEquals(1, reports.size)
            assertTrue(heapDumps.isEmpty())

            now = HEAP_DUMP_AGE_MS
            watcher.checkRetained()
            assertEquals(1, reports.size)
            assertEquals(
                listOf(
                    Report(
                        "com.example.LeakyScreen",
                        "com.example.Holder",
                        null,
                        HEAP_DUMP_AGE_MS,
                        1L,
                        RetentionEvidence.TIME_ONLY,
                    ),
                ),
                heapDumps,
            )

            now++
            watcher.checkRetained()
            assertEquals(1, heapDumps.size)
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
            reporter = { className, ownerHint, context, ageMs, count, evidence ->
                reports += Report(className, ownerHint, context, ageMs, count, evidence)
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

            assertEquals(
                listOf(
                    Report(
                        "com.example.ListenerLeak",
                        "sample.memory_leak.listener_registry",
                        context,
                        RETAINED_DELAY_MS,
                        1L,
                        RetentionEvidence.TIME_ONLY,
                    ),
                ),
                reports,
            )
        } finally {
            watcher.stop()
        }
    }

    @Test
    fun ignoresDuplicateWatchesForSameLiveObject() {
        var now = 0L
        val reports = mutableListOf<Report>()
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = RETAINED_DELAY_MS,
            clock = { now },
            reporter = { className, ownerHint, context, ageMs, count, evidence ->
                reports += Report(className, ownerHint, context, ageMs, count, evidence)
            },
        )
        val retained = Any()
        enableManualWatch(watcher)
        try {
            watcher.watch(retained, "com.example.Screen", "lifecycle.onDestroy.com.example.Screen", null)
            watcher.watch(retained, "com.example.Screen", "lifecycle.onDestroy.com.example.Screen", null)
            watcher.watch(retained, "com.example.Screen", "manual.watch", null)

            now = RETAINED_DELAY_MS
            watcher.checkRetained()

            assertEquals(
                listOf(
                    Report(
                        "com.example.Screen",
                        "lifecycle.onDestroy.com.example.Screen",
                        null,
                        RETAINED_DELAY_MS,
                        1L,
                        RetentionEvidence.TIME_ONLY,
                    ),
                ),
                reports,
            )
        } finally {
            watcher.stop()
        }
    }

    @Test
    fun reportsWatcherCapacityLossWithoutRetainingExtraObject() {
        var losses = 0L
        val watcher = ObjectRetentionWatcher(
            retainedDelayMs = RETAINED_DELAY_MS,
            maxWatchedReferences = 1,
            onCardinalityLoss = { losses += it },
        )
        val first = Any()
        val second = Any()
        enableManualWatch(watcher)
        try {
            watcher.watch(first, "first", null, null)
            watcher.watch(second, "second", null, null)

            assertEquals(1L, losses)
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
        val evidence: RetentionEvidence,
    )

    private fun enableManualWatch(watcher: ObjectRetentionWatcher) {
        val runningField = ObjectRetentionWatcher::class.java.getDeclaredField("running").apply {
            isAccessible = true
        }
        (runningField.get(watcher) as AtomicBoolean).set(true)
    }

    private companion object {
        const val RETAINED_DELAY_MS = 20_000L
        const val HEAP_DUMP_AGE_MS = 30_000L
    }
}
