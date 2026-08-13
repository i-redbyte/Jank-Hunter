package io.jankhunter.runtime.internal.io

import java.util.zip.CRC32

internal object LogGrowthWire {
    const val LIVE_BYTES = 256

    fun history(state: LogGrowthHistoryState, capturedAtMs: Long): ByteArray {
        val sessions = state.sessions.takeLast(EMBEDDED_SESSION_CAPACITY)
        val days = state.days.takeLast(EMBEDDED_DAY_CAPACITY)
        val totalBytes = HISTORY_HEADER_BYTES +
            sessions.size * HISTORY_SESSION_BYTES +
            days.size * HISTORY_DAY_BYTES
        val raw = ByteArray(totalBytes)
        copy(raw, HISTORY_MAGIC, 0)
        raw[4] = FORMAT_MAJOR.toByte()
        raw[5] = FORMAT_MINOR.toByte()
        putUInt16Le(raw, 6, HISTORY_HEADER_BYTES)
        putUInt32Le(raw, 8, totalBytes.toLong())
        putUInt16Le(raw, 12, HISTORY_SESSION_BYTES)
        putUInt16Le(raw, 14, HISTORY_DAY_BYTES)
        putUInt32Le(raw, 16, sessions.size.toLong())
        putUInt32Le(raw, 20, days.size.toLong())
        putUInt64Le(raw, 24, state.generation)
        putUInt64Le(raw, 32, capturedAtMs.coerceAtLeast(0L))

        var offset = HISTORY_HEADER_BYTES
        sessions.forEach { session ->
            putUInt64Le(raw, offset, session.idHigh xor session.idLow)
            putUInt32Le(raw, offset + 8, session.dayKey.toLong())
            putUInt32Le(raw, offset + 12, if (session.recoveredAfterInterruption) FLAG_RECOVERED.toLong() else 0L)
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
            values.forEachIndexed { index, value -> putUInt64Le(raw, offset + 16 + index * Long.SIZE_BYTES, value) }
            offset += HISTORY_SESSION_BYTES
        }
        days.forEach { day ->
            putUInt32Le(raw, offset, day.dayKey.toLong())
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
            values.forEachIndexed { index, value -> putUInt64Le(raw, offset + 8 + index * Long.SIZE_BYTES, value) }
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

    private const val FORMAT_MAJOR = 1
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
