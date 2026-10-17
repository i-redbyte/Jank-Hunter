package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Test

class ThreadSnapshotCollectorTest {
    @Test fun stopDoesNotWaitForAnInFlightReportCallback() {
        val entered = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val stopped = java.util.concurrent.CountDownLatch(1)
        val collector = ThreadSnapshotCollector(5000,
            read = { ProcThreadSnapshot(emptyList(), true, 0, null) }, clock = { 100 },
            report = { _, _, _ -> entered.countDown(); check(release.await(5, java.util.concurrent.TimeUnit.SECONDS)) })
        collector.activate()
        val reader = Thread { collector.sampleOnce() }
        val stop = Thread { collector.stop(); stopped.countDown() }
        try {
            reader.start()
            org.junit.Assert.assertTrue(entered.await(5, java.util.concurrent.TimeUnit.SECONDS))
            stop.start()
            org.junit.Assert.assertTrue("Stop inherited a report callback wait",
                stopped.await(1, java.util.concurrent.TimeUnit.SECONDS))
        } finally {
            release.countDown()
            reader.join(5_000L)
            stop.join(5_000L)
        }
    }

    @Test fun epochNotReadyDoesNotPublishAnUnenrichedStartupSnapshot() {
        var ready = false
        var published = 0
        val collector = ThreadSnapshotCollector(5000,
            read = { ProcThreadSnapshot(emptyList(), true, 0, null) },
            enrich = { snapshot, _, _ -> if (ready) snapshot else null },
            clock = { 100 }, report = { _, _, _ -> published++ },
        )
        collector.activate()
        collector.sampleOnce()
        assertEquals(0, published)
        ready = true
        collector.sampleOnce()
        assertEquals(1, published)
    }

    @Test fun JavaEnrichmentIsIncludedInScanDuration() {
        var now = 100L
        var duration = 0L
        val collector = ThreadSnapshotCollector(5000,
            read = { now += 3; ProcThreadSnapshot(emptyList(), true, 0, null) },
            enrich = { snapshot, started, _ -> assertEquals(100L, started); now += 4; snapshot },
            clock = { now }, report = { _, _, elapsed -> duration = elapsed },
        )
        collector.activate()
        collector.sampleOnce()
        assertEquals(7L, duration)
    }

    @Test fun ownerCanRegisterBeforeExactScanBoundaryAndRead() {
        val events = mutableListOf<String>()
        val collector = ThreadSnapshotCollector(5000,
            beforeScan = { events += "register" },
            read = { events += "read"; ProcThreadSnapshot(emptyList(), true, 0, null) },
            enrich = { snapshot, _, nanos -> events += "enrich:$nanos"; snapshot },
            clock = { 100 }, clockNanos = { events += "boundary"; 100_900_000 },
            report = { _, _, _ -> events += "report" },
        )
        collector.activate()
        collector.sampleOnce()
        assertEquals(listOf("register", "boundary", "read", "enrich:100900000", "report"), events)
    }

    @Test fun stopDuringReadPreventsLatePublication() {
        var published = 0
        lateinit var collector: ThreadSnapshotCollector
        collector = ThreadSnapshotCollector(5000, read = {
            collector.stop()
            ProcThreadSnapshot(emptyList(), true, 0, null)
        }, clock = { 100 }, report = { _, _, _ -> published++ })
        collector.activate()
        collector.sampleOnce()
        assertEquals(0, published)
    }

    @Test fun restartDuringReadDoesNotPublishOldGeneration() {
        var published = 0
        lateinit var collector: ThreadSnapshotCollector
        var restart = true
        collector = ThreadSnapshotCollector(5000, read = {
            if (restart) { restart = false; collector.stop(); collector.activate() }
            ProcThreadSnapshot(emptyList(), true, 0, null)
        }, clock = { 100 }, report = { _, _, _ -> published++ })
        collector.activate()
        collector.sampleOnce()
        assertEquals(0, published)
        collector.sampleOnce()
        assertEquals(1, published)
        collector.stop()
        collector.sampleOnce()
        assertEquals(1, published)
    }
}
