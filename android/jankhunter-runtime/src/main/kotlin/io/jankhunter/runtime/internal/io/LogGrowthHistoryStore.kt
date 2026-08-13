package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.IOException
import java.io.RandomAccessFile

internal class LogGrowthHistoryStore(
    directory: File,
) {
    private val file = File(directory, FILE_NAME)

    fun load(): LogGrowthHistoryState {
        if (!file.isFile) return LogGrowthHistoryState.EMPTY
        return try {
            RandomAccessFile(file, "r").use(::readState)
        } catch (_: IOException) {
            LogGrowthHistoryState.EMPTY
        }
    }

    fun writeActive(state: LogGrowthHistoryState, active: ActiveLogGrowthFact?): LogGrowthHistoryState {
        return writable(state) { access, current ->
            val next = writeActiveRecord(access, current, active)
            access.fd.sync()
            next
        }
    }

    fun complete(
        state: LogGrowthHistoryState,
        completed: LogGrowthSessionFact,
    ): LogGrowthHistoryState {
        return writable(state) { access, current ->
            if (current.sessions.any { it.idHigh == completed.idHigh && it.idLow == completed.idLow }) {
                val cleared = writeActiveRecord(access, current, null)
                access.fd.sync()
                return@writable cleared
            }
            val targetGeneration = current.generation + 1L
            val session = completed.copy(
                sequence = current.nextSessionSequence,
                commitGeneration = targetGeneration,
            )
            writeSession(access, current, session)

            val existingDay = current.days.firstOrNull { it.dayKey == session.dayKey }
            val day = aggregateDay(existingDay, session, current.nextDaySequence, targetGeneration)
            writeDay(access, current, day, existingDay)
            access.fd.sync()

            val sessions = appendBounded(current.sessions, session, SESSION_CAPACITY)
            val days = if (existingDay == null) {
                appendBounded(current.days, day, DAY_CAPACITY)
            } else {
                current.days.map { if (it.sequence == existingDay.sequence) day else it }
            }
            val next = current.copy(
                generation = targetGeneration,
                nextSessionSequence = current.nextSessionSequence + 1L,
                nextDaySequence = current.nextDaySequence + if (existingDay == null) 1L else 0L,
                sessions = sessions,
                days = days,
            )
            publishSuperblock(access, next)
            access.fd.sync()

            val cleared = writeActiveRecord(access, next, null)
            access.fd.sync()
            cleared
        }
    }

    private fun writable(
        state: LogGrowthHistoryState,
        action: (RandomAccessFile, LogGrowthHistoryState) -> LogGrowthHistoryState,
    ): LogGrowthHistoryState {
        file.parentFile?.mkdirs()
        return RandomAccessFile(file, "rw").use { access ->
            val current = ensureInitialized(access, state)
            action(access, current)
        }
    }

    private fun ensureInitialized(
        access: RandomAccessFile,
        state: LogGrowthHistoryState,
    ): LogGrowthHistoryState {
        if (hasPrefix(access) && state.generation > 0L) return state
        access.setLength(FILE_BYTES)
        writeAt(access, 0L, FILE_PREFIX)
        val initial = LogGrowthHistoryState.EMPTY.copy(generation = 1L)
        writeAt(access, SUPERBLOCK_A_OFFSET, encodeSuperblock(initial))
        writeAt(access, SUPERBLOCK_B_OFFSET, encodeSuperblock(initial))
        writeAt(access, ACTIVE_A_OFFSET, encodeActive(1L, null))
        writeAt(access, ACTIVE_B_OFFSET, encodeActive(0L, null))
        access.fd.sync()
        return initial
    }

    private fun readState(access: RandomAccessFile): LogGrowthHistoryState {
        if (!hasPrefix(access)) return LogGrowthHistoryState.EMPTY
        val superblock = listOfNotNull(
            decodeSuperblock(readAt(access, SUPERBLOCK_A_OFFSET, SUPERBLOCK_BYTES)),
            decodeSuperblock(readAt(access, SUPERBLOCK_B_OFFSET, SUPERBLOCK_BYTES)),
        ).maxByOrNull(Superblock::generation) ?: return LogGrowthHistoryState.EMPTY

        val sessionFirst = superblock.nextSessionSequence - superblock.sessionCount
        val sessions = ArrayList<LogGrowthSessionFact>(superblock.sessionCount.toInt())
        for (sequence in sessionFirst until superblock.nextSessionSequence) {
            readSession(access, sequence, superblock.generation)?.let(sessions::add)
        }
        val dayFirst = superblock.nextDaySequence - superblock.dayCount
        val days = ArrayList<LogGrowthDayFact>(superblock.dayCount.toInt())
        for (sequence in dayFirst until superblock.nextDaySequence) {
            readDay(access, sequence, superblock.generation)?.let(days::add)
        }
        return LogGrowthHistoryState(
            generation = superblock.generation,
            nextSessionSequence = superblock.nextSessionSequence,
            nextDaySequence = superblock.nextDaySequence,
            sessions = sessions,
            days = days,
            active = readActive(access),
        )
    }

    private fun readSession(
        access: RandomAccessFile,
        sequence: Long,
        maximumGeneration: Long,
    ): LogGrowthSessionFact? {
        val base = sessionOffset(sequence)
        return listOfNotNull(
            decodeSession(readAt(access, base, SESSION_RECORD_BYTES)),
            decodeSession(readAt(access, base + SESSION_RECORD_BYTES, SESSION_RECORD_BYTES)),
        ).filter { it.sequence == sequence && it.commitGeneration <= maximumGeneration }
            .maxByOrNull(LogGrowthSessionFact::commitGeneration)
    }

    private fun readDay(
        access: RandomAccessFile,
        sequence: Long,
        maximumGeneration: Long,
    ): LogGrowthDayFact? {
        val base = dayOffset(sequence)
        return listOfNotNull(
            decodeDay(readAt(access, base, DAY_RECORD_BYTES)),
            decodeDay(readAt(access, base + DAY_RECORD_BYTES, DAY_RECORD_BYTES)),
        ).filter { it.sequence == sequence && it.commitGeneration <= maximumGeneration }
            .maxByOrNull(LogGrowthDayFact::commitGeneration)
    }

    private fun readActive(access: RandomAccessFile): ActiveLogGrowthFact? {
        val records = activeRecords(access)
        val newest = records.maxByOrNull(ActiveRecord::generation) ?: return null
        return newest.fact
    }

    private fun activeRecords(access: RandomAccessFile): List<ActiveRecord> = listOfNotNull(
        decodeActive(readAt(access, ACTIVE_A_OFFSET, ACTIVE_RECORD_BYTES), 0),
        decodeActive(readAt(access, ACTIVE_B_OFFSET, ACTIVE_RECORD_BYTES), 1),
    )

    private fun writeSession(
        access: RandomAccessFile,
        state: LogGrowthHistoryState,
        session: LogGrowthSessionFact,
    ) {
        val base = sessionOffset(session.sequence)
        val protectedSequence = if (state.sessions.size == SESSION_CAPACITY) {
            session.sequence - SESSION_CAPACITY
        } else {
            -1L
        }
        val first = decodeSession(readAt(access, base, SESSION_RECORD_BYTES))
        val second = decodeSession(readAt(access, base + SESSION_RECORD_BYTES, SESSION_RECORD_BYTES))
        val slot = protectedAlternateSlot(first, second, protectedSequence, state.generation)
        writeAt(access, base + slot * SESSION_RECORD_BYTES, encodeSession(session))
    }

    private fun writeDay(
        access: RandomAccessFile,
        state: LogGrowthHistoryState,
        day: LogGrowthDayFact,
        existing: LogGrowthDayFact?,
    ) {
        val base = dayOffset(day.sequence)
        val protectedSequence = existing?.sequence ?: if (state.days.size == DAY_CAPACITY) {
            day.sequence - DAY_CAPACITY
        } else {
            -1L
        }
        val first = decodeDay(readAt(access, base, DAY_RECORD_BYTES))
        val second = decodeDay(readAt(access, base + DAY_RECORD_BYTES, DAY_RECORD_BYTES))
        val slot = protectedAlternateSlot(first, second, protectedSequence, state.generation)
        writeAt(access, base + slot * DAY_RECORD_BYTES, encodeDay(day))
    }

    private fun protectedAlternateSlot(
        first: LogGrowthSequencedRecord?,
        second: LogGrowthSequencedRecord?,
        protectedSequence: Long,
        maximumGeneration: Long,
    ): Long {
        val firstProtected = first?.sequence == protectedSequence && first.commitGeneration <= maximumGeneration
        val secondProtected = second?.sequence == protectedSequence && second.commitGeneration <= maximumGeneration
        return when {
            firstProtected && !secondProtected -> 1L
            secondProtected && !firstProtected -> 0L
            (first?.commitGeneration ?: -1L) <= (second?.commitGeneration ?: -1L) -> 0L
            else -> 1L
        }
    }

    private fun writeActiveRecord(
        access: RandomAccessFile,
        state: LogGrowthHistoryState,
        active: ActiveLogGrowthFact?,
    ): LogGrowthHistoryState {
        val previous = activeRecords(access).maxByOrNull(ActiveRecord::generation)
        val generation = maxOf(previous?.generation ?: 0L, state.active?.generation ?: 0L) + 1L
        val slot = if (previous?.slot == 0) 1 else 0
        val next = active?.copy(generation = generation)
        writeAt(access, activeOffset(slot), encodeActive(generation, next))
        return state.copy(active = next)
    }

    private fun publishSuperblock(access: RandomAccessFile, state: LogGrowthHistoryState) {
        val offset = if (state.generation and 1L == 0L) SUPERBLOCK_A_OFFSET else SUPERBLOCK_B_OFFSET
        writeAt(access, offset, encodeSuperblock(state))
    }

    private fun aggregateDay(
        previous: LogGrowthDayFact?,
        session: LogGrowthSessionFact,
        nextDaySequence: Long,
        commitGeneration: Long,
    ): LogGrowthDayFact {
        val duration = (session.endedAtMs - session.startedAtMs).coerceAtLeast(0L)
        val fillPermille = if (session.configuredLimitBytes <= 0L) {
            0L
        } else {
            saturatedMultiply(session.maximumRetainedBytes, FILL_SCALE) / session.configuredLimitBytes
        }
        return LogGrowthDayFact(
            sequence = previous?.sequence ?: nextDaySequence,
            commitGeneration = commitGeneration,
            dayKey = session.dayKey,
            sessionCount = saturatedAdd(previous?.sessionCount ?: 0L, 1L),
            totalDurationMs = saturatedAdd(previous?.totalDurationMs ?: 0L, duration),
            generatedBytes = saturatedAdd(previous?.generatedBytes ?: 0L, session.generatedBytes),
            maximumRetainedBytes = maxOf(previous?.maximumRetainedBytes ?: 0L, session.maximumRetainedBytes),
            maximumFillPermille = maxOf(previous?.maximumFillPermille ?: 0L, fillPermille),
            sessionsReachingLimit = saturatedAdd(
                previous?.sessionsReachingLimit ?: 0L,
                if (session.overflowCount > 0L) 1L else 0L,
            ),
            overflowCount = saturatedAdd(previous?.overflowCount ?: 0L, session.overflowCount),
            evictedChunkCount = saturatedAdd(previous?.evictedChunkCount ?: 0L, session.evictedChunkCount),
            evictedBytes = saturatedAdd(previous?.evictedBytes ?: 0L, session.evictedBytes),
        )
    }

    private fun encodeSuperblock(state: LogGrowthHistoryState): ByteArray {
        val raw = ByteArray(SUPERBLOCK_BYTES)
        copyMagic(raw, SUPERBLOCK_MAGIC)
        putUInt16Le(raw, 4, SCHEMA)
        putUInt64Le(raw, 8, state.generation)
        putUInt64Le(raw, 16, state.nextSessionSequence)
        putUInt64Le(raw, 24, state.sessions.size.toLong())
        putUInt64Le(raw, 32, state.nextDaySequence)
        putUInt64Le(raw, 40, state.days.size.toLong())
        putCrc(raw)
        return raw
    }

    private fun decodeSuperblock(raw: ByteArray): Superblock? {
        if (!validRecord(raw, SUPERBLOCK_MAGIC, SCHEMA)) return null
        val sessionCount = uint64Le(raw, 24)
        val dayCount = uint64Le(raw, 40)
        if (sessionCount > SESSION_CAPACITY || dayCount > DAY_CAPACITY) return null
        return Superblock(
            generation = uint64Le(raw, 8),
            nextSessionSequence = uint64Le(raw, 16),
            sessionCount = sessionCount,
            nextDaySequence = uint64Le(raw, 32),
            dayCount = dayCount,
        ).takeIf {
            it.generation > 0L &&
                it.nextSessionSequence >= it.sessionCount &&
                it.nextDaySequence >= it.dayCount
        }
    }

    private fun encodeSession(session: LogGrowthSessionFact): ByteArray {
        val raw = ByteArray(SESSION_RECORD_BYTES)
        copyMagic(raw, SESSION_MAGIC)
        putUInt16Le(raw, 4, SCHEMA)
        putUInt16Le(raw, 6, if (session.recoveredAfterInterruption) FLAG_RECOVERED else 0)
        putUInt64Le(raw, 8, session.commitGeneration)
        putUInt64Le(raw, 16, session.sequence)
        putUInt64Le(raw, 24, session.idHigh)
        putUInt64Le(raw, 32, session.idLow)
        putUInt32Le(raw, 40, session.dayKey.toLong())
        val values = longArrayOf(
            session.startedAtMs,
            session.endedAtMs,
            session.configuredLimitBytes,
            session.maximumRetainedBytes,
            session.generatedBytes,
            session.overflowCount,
            session.evictedChunkCount,
            session.evictedBytes,
            session.firstOverflowAtMs,
            session.lastOverflowAtMs,
        )
        values.forEachIndexed { index, value -> putUInt64Le(raw, 48 + index * Long.SIZE_BYTES, value) }
        putCrc(raw)
        return raw
    }

    private fun decodeSession(raw: ByteArray): LogGrowthSessionFact? {
        if (!validRecord(raw, SESSION_MAGIC, SCHEMA)) return null
        return LogGrowthSessionFact(
            sequence = uint64Le(raw, 16),
            commitGeneration = uint64Le(raw, 8),
            idHigh = uint64Le(raw, 24),
            idLow = uint64Le(raw, 32),
            dayKey = uint32Le(raw, 40).toInt(),
            startedAtMs = uint64Le(raw, 48),
            endedAtMs = uint64Le(raw, 56),
            configuredLimitBytes = uint64Le(raw, 64),
            maximumRetainedBytes = uint64Le(raw, 72),
            generatedBytes = uint64Le(raw, 80),
            overflowCount = uint64Le(raw, 88),
            evictedChunkCount = uint64Le(raw, 96),
            evictedBytes = uint64Le(raw, 104),
            firstOverflowAtMs = uint64Le(raw, 112),
            lastOverflowAtMs = uint64Le(raw, 120),
            recoveredAfterInterruption = uint16Le(raw, 6) and FLAG_RECOVERED != 0,
        )
    }

    private fun encodeDay(day: LogGrowthDayFact): ByteArray {
        val raw = ByteArray(DAY_RECORD_BYTES)
        copyMagic(raw, DAY_MAGIC)
        putUInt16Le(raw, 4, SCHEMA)
        putUInt64Le(raw, 8, day.commitGeneration)
        putUInt64Le(raw, 16, day.sequence)
        putUInt32Le(raw, 24, day.dayKey.toLong())
        val values = longArrayOf(
            day.sessionCount,
            day.totalDurationMs,
            day.generatedBytes,
            day.maximumRetainedBytes,
            day.maximumFillPermille,
            day.sessionsReachingLimit,
            day.overflowCount,
            day.evictedChunkCount,
            day.evictedBytes,
        )
        values.forEachIndexed { index, value -> putUInt64Le(raw, 32 + index * Long.SIZE_BYTES, value) }
        putCrc(raw)
        return raw
    }

    private fun decodeDay(raw: ByteArray): LogGrowthDayFact? {
        if (!validRecord(raw, DAY_MAGIC, SCHEMA)) return null
        return LogGrowthDayFact(
            sequence = uint64Le(raw, 16),
            commitGeneration = uint64Le(raw, 8),
            dayKey = uint32Le(raw, 24).toInt(),
            sessionCount = uint64Le(raw, 32),
            totalDurationMs = uint64Le(raw, 40),
            generatedBytes = uint64Le(raw, 48),
            maximumRetainedBytes = uint64Le(raw, 56),
            maximumFillPermille = uint64Le(raw, 64),
            sessionsReachingLimit = uint64Le(raw, 72),
            overflowCount = uint64Le(raw, 80),
            evictedChunkCount = uint64Le(raw, 88),
            evictedBytes = uint64Le(raw, 96),
        )
    }

    private fun encodeActive(generation: Long, fact: ActiveLogGrowthFact?): ByteArray {
        val raw = ByteArray(ACTIVE_RECORD_BYTES)
        copyMagic(raw, ACTIVE_MAGIC)
        putUInt16Le(raw, 4, SCHEMA)
        putUInt16Le(raw, 6, if (fact == null) 0 else FLAG_ACTIVE)
        putUInt64Le(raw, 8, generation)
        if (fact != null) {
            putUInt64Le(raw, 16, fact.idHigh)
            putUInt64Le(raw, 24, fact.idLow)
            putUInt32Le(raw, 32, fact.dayKey.toLong())
            val values = longArrayOf(
                fact.startedAtMs,
                fact.updatedAtMs,
                fact.configuredLimitBytes,
                fact.maximumRetainedBytes,
                fact.generatedBytes,
                fact.overflowCount,
                fact.evictedChunkCount,
                fact.evictedBytes,
                fact.firstOverflowAtMs,
                fact.lastOverflowAtMs,
            )
            values.forEachIndexed { index, value -> putUInt64Le(raw, 40 + index * Long.SIZE_BYTES, value) }
        }
        putCrc(raw)
        return raw
    }

    private fun decodeActive(raw: ByteArray, slot: Int): ActiveRecord? {
        if (!validRecord(raw, ACTIVE_MAGIC, SCHEMA)) return null
        val generation = uint64Le(raw, 8)
        val fact = if (uint16Le(raw, 6) and FLAG_ACTIVE == 0) {
            null
        } else {
            ActiveLogGrowthFact(
                generation = generation,
                idHigh = uint64Le(raw, 16),
                idLow = uint64Le(raw, 24),
                dayKey = uint32Le(raw, 32).toInt(),
                startedAtMs = uint64Le(raw, 40),
                updatedAtMs = uint64Le(raw, 48),
                configuredLimitBytes = uint64Le(raw, 56),
                maximumRetainedBytes = uint64Le(raw, 64),
                generatedBytes = uint64Le(raw, 72),
                overflowCount = uint64Le(raw, 80),
                evictedChunkCount = uint64Le(raw, 88),
                evictedBytes = uint64Le(raw, 96),
                firstOverflowAtMs = uint64Le(raw, 104),
                lastOverflowAtMs = uint64Le(raw, 112),
            )
        }
        return ActiveRecord(generation, fact, slot)
    }

    private fun hasPrefix(access: RandomAccessFile): Boolean {
        if (access.length() < FILE_PREFIX.size) return false
        return readAt(access, 0L, FILE_PREFIX.size).contentEquals(FILE_PREFIX)
    }

    private fun sessionOffset(sequence: Long): Long =
        SESSION_OFFSET + (sequence % SESSION_CAPACITY) * SESSION_RECORD_BYTES * 2L

    private fun dayOffset(sequence: Long): Long =
        DAY_OFFSET + (sequence % DAY_CAPACITY) * DAY_RECORD_BYTES * 2L

    private fun activeOffset(slot: Int): Long = if (slot == 0) ACTIVE_A_OFFSET else ACTIVE_B_OFFSET

    private fun readAt(access: RandomAccessFile, offset: Long, size: Int): ByteArray {
        val raw = ByteArray(size)
        if (offset < 0L || offset + size > access.length()) return raw
        access.seek(offset)
        access.readFully(raw)
        return raw
    }

    private fun writeAt(access: RandomAccessFile, offset: Long, bytes: ByteArray) {
        access.seek(offset)
        access.write(bytes)
    }

    private fun validRecord(raw: ByteArray, magic: ByteArray, schema: Int): Boolean {
        if (raw.size < CRC_BYTES || !raw.startsWith(magic) || uint16Le(raw, 4) != schema) return false
        return uint32Le(raw, raw.size - CRC_BYTES) == crc32(raw, 0, raw.size - CRC_BYTES)
    }

    private fun putCrc(raw: ByteArray) {
        putUInt32Le(raw, raw.size - CRC_BYTES, crc32(raw, 0, raw.size - CRC_BYTES))
    }

    private fun copyMagic(target: ByteArray, magic: ByteArray) =
        System.arraycopy(magic, 0, target, 0, magic.size)

    private fun ByteArray.startsWith(prefix: ByteArray): Boolean {
        if (size < prefix.size) return false
        for (index in prefix.indices) if (this[index] != prefix[index]) return false
        return true
    }

    private fun <T> appendBounded(values: List<T>, value: T, capacity: Int): List<T> {
        val start = if (values.size >= capacity) values.size - capacity + 1 else 0
        return ArrayList<T>(minOf(capacity, values.size + 1)).apply {
            for (index in start until values.size) add(values[index])
            add(value)
        }
    }

    private data class Superblock(
        val generation: Long,
        val nextSessionSequence: Long,
        val sessionCount: Long,
        val nextDaySequence: Long,
        val dayCount: Long,
    )

    private data class ActiveRecord(
        val generation: Long,
        val fact: ActiveLogGrowthFact?,
        val slot: Int,
    )

    private companion object {
        const val FILE_NAME = "jh-log-growth.bin"
        const val SCHEMA = 1
        const val CRC_BYTES = Int.SIZE_BYTES
        const val FLAG_ACTIVE = 1
        const val FLAG_RECOVERED = 1
        const val FILL_SCALE = 1_000L

        const val SUPERBLOCK_BYTES = 256
        const val ACTIVE_RECORD_BYTES = 128
        const val SESSION_RECORD_BYTES = 132
        const val DAY_RECORD_BYTES = 108
        const val SESSION_CAPACITY = 1_024
        const val DAY_CAPACITY = 400

        const val SUPERBLOCK_A_OFFSET = 1_024L
        const val SUPERBLOCK_B_OFFSET = SUPERBLOCK_A_OFFSET + SUPERBLOCK_BYTES
        const val ACTIVE_A_OFFSET = SUPERBLOCK_B_OFFSET + SUPERBLOCK_BYTES
        const val ACTIVE_B_OFFSET = ACTIVE_A_OFFSET + ACTIVE_RECORD_BYTES
        const val SESSION_OFFSET = 4L * 1_024L
        const val DAY_OFFSET = SESSION_OFFSET + SESSION_CAPACITY * SESSION_RECORD_BYTES * 2L
        const val FILE_BYTES = DAY_OFFSET + DAY_CAPACITY * DAY_RECORD_BYTES * 2L

        val FILE_PREFIX = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'H'.code.toByte(), 1, 0)
        val SUPERBLOCK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'S'.code.toByte())
        val ACTIVE_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'A'.code.toByte())
        val SESSION_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'R'.code.toByte())
        val DAY_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'D'.code.toByte())
    }
}
