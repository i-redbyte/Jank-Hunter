package io.jankhunter.runtime.internal.io

import java.util.zip.CRC32

internal object LogGrowthWire {
    const val LIVE_BYTES = 256
    const val HISTORY_MAX_BYTES = 64 * 1024

    fun history(state: LogGrowthHistoryState, capturedAtMs: Long): ByteArray {
        val sessionStart = (state.sessions.size - EMBEDDED_SESSION_CAPACITY).coerceAtLeast(0)
        val dayStart = (state.days.size - EMBEDDED_DAY_CAPACITY).coerceAtLeast(0)
        val sessionCount = state.sessions.size - sessionStart
        val dayCount = state.days.size - dayStart
        val totalBytes = HISTORY_HEADER_BYTES +
            sessionCount * HISTORY_SESSION_BYTES +
            dayCount * HISTORY_DAY_BYTES
        val raw = ByteArray(totalBytes)
        copy(raw, HISTORY_MAGIC, 0)
        raw[4] = FORMAT_MAJOR.toByte()
        raw[5] = FORMAT_MINOR.toByte()
        putUInt16Le(raw, 6, HISTORY_HEADER_BYTES)
        putUInt32Le(raw, 8, totalBytes.toLong())
        putUInt16Le(raw, 12, HISTORY_SESSION_BYTES)
        putUInt16Le(raw, 14, HISTORY_DAY_BYTES)
        putUInt32Le(raw, 16, sessionCount.toLong())
        putUInt32Le(raw, 20, dayCount.toLong())
        putUInt64Le(raw, 24, state.generation)
        putUInt64Le(raw, 32, capturedAtMs.coerceAtLeast(0L))

        var offset = HISTORY_HEADER_BYTES
        for (index in sessionStart until state.sessions.size) {
            val session = state.sessions[index]
            putUInt64Le(raw, offset, session.idHigh xor session.idLow)
            putUInt32Le(raw, offset + 8, session.dayKey.toLong())
            putUInt32Le(raw, offset + 12, if (session.recoveredAfterInterruption) FLAG_RECOVERED.toLong() else 0L)
            putUInt64Le(raw, offset + 16, session.startedAtMs)
            putUInt64Le(raw, offset + 24, session.endedAtMs)
            putUInt64Le(raw, offset + 32, session.configuredLimitBytes)
            putUInt64Le(raw, offset + 40, session.maximumRetainedBytes)
            putUInt64Le(raw, offset + 48, session.generatedBytes)
            putUInt64Le(raw, offset + 56, session.limitReachedCount)
            putUInt64Le(raw, offset + 64, session.segmentRotationCount)
            putUInt64Le(raw, offset + 72, session.archiveEvictedBytes)
            putUInt64Le(raw, offset + 80, session.firstLimitReachedAtMs)
            putUInt64Le(raw, offset + 88, session.lastLimitReachedAtMs)
            offset += HISTORY_SESSION_BYTES
        }
        for (index in dayStart until state.days.size) {
            val day = state.days[index]
            putUInt32Le(raw, offset, day.dayKey.toLong())
            putUInt64Le(raw, offset + 8, day.sessionCount)
            putUInt64Le(raw, offset + 16, day.totalDurationMs)
            putUInt64Le(raw, offset + 24, day.generatedBytes)
            putUInt64Le(raw, offset + 32, day.maximumRetainedBytes)
            putUInt64Le(raw, offset + 40, day.maximumFillPermille)
            putUInt64Le(raw, offset + 48, day.sessionsReachingLimit)
            putUInt64Le(raw, offset + 56, day.limitReachedCount)
            putUInt64Le(raw, offset + 64, day.segmentRotationCount)
            putUInt64Le(raw, offset + 72, day.archiveEvictedBytes)
            offset += HISTORY_DAY_BYTES
        }
        putUInt32Le(raw, HISTORY_CRC_OFFSET, crcWithZeroedField(raw, HISTORY_CRC_OFFSET))
        return raw
    }

    fun live(fact: ActiveLogGrowthFact, completed: Boolean): ByteArray {
        val raw = ByteArray(LIVE_BYTES)
        copy(raw, LIVE_MAGIC, 0)
        raw[4] = FORMAT_MAJOR.toByte()
        raw[5] = FORMAT_MINOR.toByte()
        putUInt16Le(raw, 6, LIVE_BYTES)
        putUInt64Le(raw, 8, fact.generation)
        putUInt64Le(raw, 16, fact.idHigh)
        putUInt64Le(raw, 24, fact.idLow)
        putUInt32Le(raw, 32, fact.dayKey.toLong())
        putUInt32Le(raw, 36, if (completed) FLAG_COMPLETED.toLong() else 0L)
        putUInt64Le(raw, 40, fact.startedAtMs)
        putUInt64Le(raw, 48, fact.updatedAtMs)
        putUInt64Le(raw, 56, fact.configuredLimitBytes)
        putUInt64Le(raw, 64, fact.maximumRetainedBytes)
        putUInt64Le(raw, 72, fact.generatedBytes)
        putUInt64Le(raw, 80, fact.limitReachedCount)
        putUInt64Le(raw, 88, fact.segmentRotationCount)
        putUInt64Le(raw, 96, fact.archiveEvictedBytes)
        putUInt64Le(raw, 104, fact.firstLimitReachedAtMs)
        putUInt64Le(raw, 112, fact.lastLimitReachedAtMs)
        putUInt32Le(raw, raw.size - Int.SIZE_BYTES, crc32(raw, 0, raw.size - Int.SIZE_BYTES))
        return raw
    }

    private fun crcWithZeroedField(raw: ByteArray, fieldOffset: Int): Long {
        val crc = CRC32()
        crc.update(raw, 0, fieldOffset)
        crc.update(ZERO_CRC)
        crc.update(raw, fieldOffset + Int.SIZE_BYTES, raw.size - fieldOffset - Int.SIZE_BYTES)
        return crc.value
    }

    private fun copy(target: ByteArray, source: ByteArray, offset: Int) =
        System.arraycopy(source, 0, target, offset, source.size)

    private const val FORMAT_MAJOR = 2
    private const val FORMAT_MINOR = 0
    private const val HISTORY_HEADER_BYTES = 64
    private const val HISTORY_SESSION_BYTES = 96
    private const val HISTORY_DAY_BYTES = 80
    private const val HISTORY_CRC_OFFSET = 60
    private const val EMBEDDED_SESSION_CAPACITY = 256
    private const val EMBEDDED_DAY_CAPACITY = 400
    private const val FLAG_RECOVERED = 1
    private const val FLAG_COMPLETED = 1
    private val ZERO_CRC = ByteArray(Int.SIZE_BYTES)
    private val HISTORY_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'P'.code.toByte())
    private val LIVE_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'G'.code.toByte(), 'L'.code.toByte())
}
