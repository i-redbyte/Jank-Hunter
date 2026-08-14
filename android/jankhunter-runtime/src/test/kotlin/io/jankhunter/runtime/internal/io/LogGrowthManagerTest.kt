package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LogGrowthManagerTest {
    @Test
    fun embeddedProjectionStaysWithinReservedAreaAtApprovedCapacities() {
        val sessions = List(1_024) { index -> sessionFact(index.toLong()) }
        val days = List(400) { index -> dayFact(index.toLong()) }

        val projection = LogGrowthWire.history(
            LogGrowthHistoryState(
                generation = 10L,
                nextSessionSequence = sessions.size.toLong(),
                nextDaySequence = days.size.toLong(),
                sessions = sessions,
                days = days,
                active = null,
            ),
            capturedAtMs = 1_800_000_000_000L,
        )

        assertEquals(64 + 256 * 96 + 400 * 80, projection.size)
        assertTrue(projection.size <= JhlogV1.HISTORY_MAX_BYTES)
    }

    @Test
    fun persistsSessionAndDayFactsOnlyAtBoundaries() {
        val directory = Files.createTempDirectory("jankhunter-growth-history").toFile()
        var now = 1_800_000_000_000L
        try {
            val manager = LogGrowthManager(directory) { now }
            val started = manager.beginSession(
                sessionId = sessionId(1),
                localDate = "2027-01-15",
                startedAtMs = now,
                configuredLimitBytes = 1_048_576L,
                stats = stats(retained = 81_920L, generated = 81_920L),
            )
            assertTrue(started.history.size <= JhlogV1.HISTORY_MAX_BYTES)
            assertEquals(LogGrowthWire.LIVE_BYTES, started.live.size)
            assertNull(manager.onChunkCommitted(stats(retained = 200_000L, generated = 300_000L)))

            now += 60_000L
            val overflowLive = manager.onChunkCommitted(
                stats(
                    retained = 1_048_576L,
                    generated = 1_300_000L,
                    overflows = 2L,
                    evictedChunks = 3L,
                    evictedBytes = 250_000L,
                ),
            )
            assertNotNull(overflowLive)
            val current = requireNotNull(manager.summary().currentSession)
            assertEquals(2L, current.overflowCount)
            assertTrue(current.reachedLimit)

            now += 60_000L
            assertNotNull(
                manager.complete(
                    stats(
                        retained = 1_048_576L,
                        generated = 1_600_000L,
                        overflows = 2L,
                        evictedChunks = 3L,
                        evictedBytes = 250_000L,
                    ),
                ),
            )

            val persisted = LogGrowthManager(directory) { now }.summary()
            assertNull(persisted.currentSession)
            assertEquals(1, persisted.recentSessions.size)
            assertEquals("15.01.2027", persisted.recentSessions.single().localDate)
            assertEquals(1_600_000L, persisted.recentSessions.single().generatedBytes)
            assertEquals(120_000L, persisted.recentSessions.single().durationMs)
            assertEquals(1, persisted.days.size)
            assertEquals("15.01.2027", persisted.days.single().localDate)
            assertEquals(2L, persisted.days.single().overflowCount)
            assertEquals(1L, persisted.days.single().sessionsReachingLimit)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun recoversInterruptedActiveSessionExactlyOnce() {
        val directory = Files.createTempDirectory("jankhunter-growth-recovery").toFile()
        var now = 1_800_000_000_000L
        try {
            LogGrowthManager(directory) { now }.run {
                beginSession(sessionId(1), "2027-01-15", now, 1_048_576L, stats(81_920L, 81_920L))
                now += 30_000L
                checkpoint(stats(400_000L, 500_000L))
            }

            val restarted = LogGrowthManager(directory) { now }
            restarted.beginSession(sessionId(2), "2027-01-15", now, 1_048_576L, stats(81_920L, 81_920L))
            val summary = restarted.summary()
            assertEquals(1, summary.recentSessions.size)
            assertTrue(summary.recentSessions.single().recoveredAfterInterruption)
            assertEquals(500_000L, summary.recentSessions.single().generatedBytes)

            val loadedAgain = LogGrowthManager(directory) { now }.summary()
            assertEquals(1, loadedAgain.recentSessions.size)
            assertTrue(loadedAgain.recentSessions.single().completed)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun fallsBackToPreviousSuperblockAfterChecksumDamage() {
        val directory = Files.createTempDirectory("jankhunter-growth-superblock").toFile()
        var now = 1_800_000_000_000L
        try {
            val manager = LogGrowthManager(directory) { now }
            completeSession(manager, 1, now)
            now += 60_000L
            completeSession(manager, 2, now)

            val history = File(directory, "jh-log-growth.bin")
            RandomAccessFile(history, "rw").use { access ->
                val position = 1_280L + 255L
                access.seek(position)
                val damaged = access.read().xor(0xff)
                access.seek(position)
                access.write(damaged)
            }

            val recovered = LogGrowthManager(directory) { now }.summary()
            assertEquals(1, recovered.recentSessions.size)
            assertTrue(recovered.recentSessions.single().sessionId.endsWith("0000000000000000"))
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun completeSession(manager: LogGrowthManager, id: Int, startedAtMs: Long) {
        manager.beginSession(sessionId(id), "2027-01-15", startedAtMs, 1_048_576L, stats(81_920L, 81_920L))
        manager.complete(stats(200_000L, 220_000L))
    }

    private fun sessionId(value: Int): ByteArray = ByteArray(16).also { it[0] = value.toByte() }

    private fun sessionFact(sequence: Long): LogGrowthSessionFact = LogGrowthSessionFact(
        sequence = sequence,
        commitGeneration = 1L,
        idHigh = sequence,
        idLow = 0L,
        dayKey = 20270115,
        startedAtMs = 1L,
        endedAtMs = 2L,
        configuredLimitBytes = 3L,
        maximumRetainedBytes = 4L,
        generatedBytes = 5L,
        overflowCount = 6L,
        evictedChunkCount = 7L,
        evictedBytes = 8L,
        firstOverflowAtMs = 9L,
        lastOverflowAtMs = 10L,
        recoveredAfterInterruption = false,
    )

    private fun dayFact(sequence: Long): LogGrowthDayFact = LogGrowthDayFact(
        sequence = sequence,
        commitGeneration = 1L,
        dayKey = 20270115,
        sessionCount = 1L,
        totalDurationMs = 2L,
        generatedBytes = 3L,
        maximumRetainedBytes = 4L,
        maximumFillPermille = 5L,
        sessionsReachingLimit = 6L,
        overflowCount = 7L,
        evictedChunkCount = 8L,
        evictedBytes = 9L,
    )

    private fun stats(
        retained: Long,
        generated: Long,
        overflows: Long = 0L,
        evictedChunks: Long = 0L,
        evictedBytes: Long = 0L,
    ): LogContainerStats = LogContainerStats(retained, generated, overflows, evictedChunks, evictedBytes)
}
