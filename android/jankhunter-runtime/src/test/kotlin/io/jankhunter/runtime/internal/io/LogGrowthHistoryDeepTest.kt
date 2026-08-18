package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class LogGrowthHistoryDeepTest {
    @Test
    fun diskHistoryRetainsExactNewestWindowAcrossBothRingBoundaries() {
        val directory = Files.createTempDirectory("jankhunter-growth-capacity").toFile()
        try {
            val store = LogGrowthHistoryStore(directory)
            var state = LogGrowthHistoryState.EMPTY
            val firstDay = LocalDate.of(2027, 1, 1)
            val total = SESSION_CAPACITY + 1

            repeat(total) { index ->
                state = store.complete(
                    state,
                    completedFact(
                        id = index,
                        localDate = firstDay.plusDays(index.toLong()).toString(),
                        generatedBytes = 10_000L + index,
                    ),
                )
            }

            assertEquals(SESSION_CAPACITY, state.sessions.size)
            assertEquals(DAY_CAPACITY, state.days.size)
            assertEquals(1L, state.sessions.first().sequence)
            assertEquals(SESSION_CAPACITY.toLong(), state.sessions.last().sequence)
            assertEquals((total - DAY_CAPACITY).toLong(), state.days.first().sequence)
            assertEquals(SESSION_CAPACITY.toLong(), state.days.last().sequence)
            assertEquals(10_001L, state.sessions.first().generatedBytes)
            assertEquals(10_000L + SESSION_CAPACITY, state.sessions.last().generatedBytes)
            assertEquals("17.09.2028", dayKeyToDisplayString(state.days.first().dayKey))
            assertEquals("21.10.2029", dayKeyToDisplayString(state.days.last().dayKey))

            val reloaded = LogGrowthHistoryStore(directory).load()
            assertEquals(state, reloaded)
            assertEquals(HISTORY_FILE_BYTES, File(directory, HISTORY_FILE_NAME).length())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun damagedNewestActiveRecordFallsBackWithoutInventingNewerCounters() {
        val directory = Files.createTempDirectory("jankhunter-growth-active-fallback").toFile()
        var now = 1_800_000_000_000L
        try {
            val manager = LogGrowthManager(directory) { now }
            manager.beginSession(sessionId(1), "2027-01-15", now, LIMIT_BYTES, stats(10L, 10L))
            now += 1_000L
            manager.checkpoint(stats(20L, 20L, limitReached = 1L, rotations = 1L, archiveEvicted = 2L))
            now += 1_000L
            manager.checkpoint(stats(30L, 30L, limitReached = 2L, rotations = 2L, archiveEvicted = 4L))

            damageByte(File(directory, HISTORY_FILE_NAME), ACTIVE_B_OFFSET + ACTIVE_RECORD_BYTES - 1L)

            val recovered = requireNotNull(LogGrowthManager(directory) { now }.summary().currentSession)
            assertEquals(20L, recovered.generatedBytes)
            assertEquals(20L, recovered.maximumRetainedBytes)
            assertEquals(1L, recovered.limitReachedCount)
            assertEquals(1L, recovered.segmentRotationCount)
            assertEquals(2L, recovered.archiveEvictedBytes)
            assertFalse(recovered.completed)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun damagedClearMarkerCannotDuplicateAnAlreadyCompletedSession() {
        val directory = Files.createTempDirectory("jankhunter-growth-clear-fallback").toFile()
        var now = 1_800_000_000_000L
        try {
            val manager = LogGrowthManager(directory) { now }
            manager.beginSession(sessionId(1), "2027-01-15", now, LIMIT_BYTES, stats(10L, 10L))
            now += 1_000L
            manager.complete(stats(20L, 20L))

            damageByte(File(directory, HISTORY_FILE_NAME), ACTIVE_B_OFFSET + ACTIVE_RECORD_BYTES - 1L)

            val restarted = LogGrowthManager(directory) { now }
            now += 1_000L
            restarted.beginSession(sessionId(2), "2027-01-15", now, LIMIT_BYTES, stats(1L, 1L))
            val summary = restarted.summary()
            assertEquals(1, summary.recentSessions.size)
            assertEquals(sessionIdHex(1), summary.recentSessions.single().sessionId)
            assertEquals(sessionIdHex(2), requireNotNull(summary.currentSession).sessionId)

            val loadedAgain = LogGrowthManager(directory) { now }.summary()
            assertEquals(1, loadedAgain.recentSessions.size)
            assertEquals(sessionIdHex(1), loadedAgain.recentSessions.single().sessionId)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun countersAreMonotonicAndDayTotalsSaturateInsteadOfOverflowing() {
        val directory = Files.createTempDirectory("jankhunter-growth-saturation").toFile()
        var now = 1_800_000_000_000L
        try {
            val manager = LogGrowthManager(directory) { now }
            manager.beginSession(sessionId(1), "2027-01-15", now, 1L, stats(0L, 0L))
            now += 1_000L
            manager.checkpoint(
                stats(
                    retained = Long.MAX_VALUE - 1L,
                    generated = Long.MAX_VALUE - 1L,
                    limitReached = Long.MAX_VALUE - 1L,
                    rotations = Long.MAX_VALUE - 1L,
                    archiveEvicted = Long.MAX_VALUE - 1L,
                ),
            )
            now += 1_000L
            manager.checkpoint(stats(1L, 1L, limitReached = 1L, rotations = 1L, archiveEvicted = 1L))
            val first = requireNotNull(manager.summary().currentSession)
            assertEquals(Long.MAX_VALUE - 1L, first.generatedBytes)
            assertEquals(Long.MAX_VALUE - 1L, first.limitReachedCount)
            assertEquals(now - 1_000L, first.firstLimitReachedAtMs)
            assertEquals(now - 1_000L, first.lastLimitReachedAtMs)
            manager.complete(stats(Long.MAX_VALUE, Long.MAX_VALUE, Long.MAX_VALUE, Long.MAX_VALUE, Long.MAX_VALUE))

            now += 1_000L
            manager.beginSession(sessionId(2), "2027-01-15", now, 1L, stats(0L, 0L))
            now += 1_000L
            manager.complete(stats(100L, 100L, 10L, 10L, 100L))

            val summary = manager.summary()
            assertNull(summary.currentSession)
            val day = summary.days.single()
            assertEquals(2L, day.sessionCount)
            assertEquals(Long.MAX_VALUE, day.generatedBytes)
            assertEquals(Long.MAX_VALUE, day.maximumRetainedBytes)
            assertEquals(Long.MAX_VALUE, day.maximumFillPermille)
            assertEquals(2L, day.sessionsReachingLimit)
            assertEquals(Long.MAX_VALUE, day.limitReachedCount)
            assertEquals(Long.MAX_VALUE, day.segmentRotationCount)
            assertEquals(Long.MAX_VALUE, day.archiveEvictedBytes)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun everyHardLimitSignalUpdatesOnlyTheLastLimitTime() {
        val directory = Files.createTempDirectory("jankhunter-growth-limit-reached").toFile()
        var now = 1_800_000_000_000L
        try {
            val manager = LogGrowthManager(directory) { now }
            manager.beginSession(sessionId(1), "2027-01-15", now, LIMIT_BYTES, stats(0L, 0L))
            val startedAt = now

            now += 1_000L
            manager.checkpoint(stats(10L, 10L, limitReached = 1L))
            now += 1_000L
            manager.checkpoint(stats(20L, 20L, limitReached = 1L))
            now += 1_000L
            manager.checkpoint(stats(30L, 30L, limitReached = 4L))
            now += 1_000L
            manager.complete(stats(40L, 40L, limitReached = 7L))

            val completed = manager.summary().recentSessions.single()
            assertEquals(7L, completed.limitReachedCount)
            assertEquals(startedAt + 1_000L, completed.firstLimitReachedAtMs)
            assertEquals(startedAt + 4_000L, completed.lastLimitReachedAtMs)
            assertTrue(completed.reachedLimit)
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun completedFact(id: Int, localDate: String, generatedBytes: Long): LogGrowthSessionFact {
        val (high, low) = sessionIdParts(sessionId(id))
        return LogGrowthSessionFact(
            sequence = -1L,
            commitGeneration = -1L,
            idHigh = high,
            idLow = low,
            dayKey = dayKey(localDate),
            startedAtMs = id * 10L,
            endedAtMs = id * 10L + 5L,
            configuredLimitBytes = LIMIT_BYTES,
            maximumRetainedBytes = generatedBytes.coerceAtMost(LIMIT_BYTES),
            generatedBytes = generatedBytes,
            limitReachedCount = 0L,
            segmentRotationCount = 0L,
            archiveEvictedBytes = 0L,
            firstLimitReachedAtMs = 0L,
            lastLimitReachedAtMs = 0L,
            recoveredAfterInterruption = false,
        )
    }

    private fun stats(
        retained: Long,
        generated: Long,
        limitReached: Long = 0L,
        rotations: Long = 0L,
        archiveEvicted: Long = 0L,
    ): LogContainerStats = LogContainerStats(retained, generated, limitReached, rotations, archiveEvicted)

    private fun sessionId(value: Int): ByteArray = ByteArray(16).also { result ->
        repeat(Int.SIZE_BYTES) { index -> result[index] = (value ushr (index * Byte.SIZE_BITS)).toByte() }
    }

    private fun sessionIdHex(value: Int): String = buildString(32) {
        sessionId(value).forEach { byte -> append("%02x".format(byte.toInt() and 0xff)) }
    }

    private fun damageByte(file: File, offset: Long) {
        RandomAccessFile(file, "rw").use { access ->
            access.seek(offset)
            val damaged = access.read().xor(0xff)
            access.seek(offset)
            access.write(damaged)
        }
    }

    private companion object {
        const val LIMIT_BYTES = 1_048_576L
        const val SESSION_CAPACITY = 1_024
        const val DAY_CAPACITY = 400
        const val ACTIVE_B_OFFSET = 1_664L
        const val ACTIVE_RECORD_BYTES = 128L
        const val HISTORY_FILE_BYTES = 360_832L
        const val HISTORY_FILE_NAME = "jh-log-growth.bin"
    }
}
