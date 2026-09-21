package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogGrowthDaySummary
import io.jankhunter.runtime.JankHunterLogGrowthSessionSummary

internal interface LogGrowthSequencedRecord {
    val sequence: Long
    val commitGeneration: Long
}

internal data class LogGrowthSessionFact(
    override val sequence: Long,
    override val commitGeneration: Long,
    val idHigh: Long,
    val idLow: Long,
    val dayKey: Int,
    val startedAtMs: Long,
    val endedAtMs: Long,
    val configuredLimitBytes: Long,
    val maximumRetainedBytes: Long,
    val generatedBytes: Long,
    val limitReachedCount: Long,
    val segmentRotationCount: Long,
    val archiveEvictedBytes: Long,
    val firstLimitReachedAtMs: Long,
    val lastLimitReachedAtMs: Long,
    val recoveredAfterInterruption: Boolean,
) : LogGrowthSequencedRecord {
    fun toPublic(completed: Boolean = true): JankHunterLogGrowthSessionSummary {
        val durationMs = (endedAtMs - startedAtMs).coerceAtLeast(0L)
        val rate = if (durationMs <= 0L) 0L else saturatedMultiply(generatedBytes, MILLIS_PER_MINUTE) / durationMs
        return JankHunterLogGrowthSessionSummary(
            sessionId = sessionIdHex(idHigh, idLow),
            localDate = dayKeyToDisplayString(dayKey),
            startedAtMs = startedAtMs,
            endedAtMs = endedAtMs,
            durationMs = durationMs,
            configuredLimitBytes = configuredLimitBytes,
            maximumRetainedBytes = maximumRetainedBytes,
            generatedBytes = generatedBytes,
            averageGrowthBytesPerMinute = rate,
            reachedLimit = limitReachedCount > 0L,
            limitReachedCount = limitReachedCount,
            segmentRotationCount = segmentRotationCount,
            archiveEvictedBytes = archiveEvictedBytes,
            firstLimitReachedAtMs = firstLimitReachedAtMs,
            lastLimitReachedAtMs = lastLimitReachedAtMs,
            completed = completed,
            recoveredAfterInterruption = recoveredAfterInterruption,
        )
    }
}

internal data class LogGrowthDayFact(
    override val sequence: Long,
    override val commitGeneration: Long,
    val dayKey: Int,
    val sessionCount: Long,
    val totalDurationMs: Long,
    val generatedBytes: Long,
    val maximumRetainedBytes: Long,
    val maximumFillPermille: Long,
    val sessionsReachingLimit: Long,
    val limitReachedCount: Long,
    val segmentRotationCount: Long,
    val archiveEvictedBytes: Long,
) : LogGrowthSequencedRecord {
    fun toPublic(): JankHunterLogGrowthDaySummary = JankHunterLogGrowthDaySummary(
        localDate = dayKeyToDisplayString(dayKey),
        sessionCount = sessionCount,
        totalDurationMs = totalDurationMs,
        generatedBytes = generatedBytes,
        maximumRetainedBytes = maximumRetainedBytes,
        maximumFillPermille = maximumFillPermille,
        sessionsReachingLimit = sessionsReachingLimit,
        limitReachedCount = limitReachedCount,
        segmentRotationCount = segmentRotationCount,
        archiveEvictedBytes = archiveEvictedBytes,
    )
}

internal data class ActiveLogGrowthFact(
    val generation: Long,
    val idHigh: Long,
    val idLow: Long,
    val dayKey: Int,
    val startedAtMs: Long,
    val updatedAtMs: Long,
    val configuredLimitBytes: Long,
    val maximumRetainedBytes: Long,
    val generatedBytes: Long,
    val limitReachedCount: Long,
    val segmentRotationCount: Long,
    val archiveEvictedBytes: Long,
    val firstLimitReachedAtMs: Long,
    val lastLimitReachedAtMs: Long,
) {
    fun toSessionFact(
        sequence: Long,
        commitGeneration: Long,
        recovered: Boolean,
    ): LogGrowthSessionFact = LogGrowthSessionFact(
        sequence = sequence,
        commitGeneration = commitGeneration,
        idHigh = idHigh,
        idLow = idLow,
        dayKey = dayKey,
        startedAtMs = startedAtMs,
        endedAtMs = updatedAtMs,
        configuredLimitBytes = configuredLimitBytes,
        maximumRetainedBytes = maximumRetainedBytes,
        generatedBytes = generatedBytes,
        limitReachedCount = limitReachedCount,
        segmentRotationCount = segmentRotationCount,
        archiveEvictedBytes = archiveEvictedBytes,
        firstLimitReachedAtMs = firstLimitReachedAtMs,
        lastLimitReachedAtMs = lastLimitReachedAtMs,
        recoveredAfterInterruption = recovered,
    )
}

internal data class LogGrowthHistoryState(
    val generation: Long,
    val nextSessionSequence: Long,
    val nextDaySequence: Long,
    val sessions: List<LogGrowthSessionFact>,
    val days: List<LogGrowthDayFact>,
    val active: ActiveLogGrowthFact?,
) {
    companion object {
        val EMPTY = LogGrowthHistoryState(0L, 0L, 0L, emptyList(), emptyList(), null)
    }
}

internal data class LogContainerStats(
    val retainedBytes: Long,
    val generatedBytes: Long,
    val limitReachedCount: Long,
    val segmentRotationCount: Long,
    val archiveEvictedBytes: Long,
) {
    companion object {
        val EMPTY = LogContainerStats(0L, 0L, 0L, 0L, 0L)
    }
}

internal fun dayKey(localDate: String): Int {
    if (localDate.length != DATE_LENGTH) return 0
    var value = 0
    for (index in localDate.indices) {
        if (index == 4 || index == 7) {
            if (localDate[index] != '-') return 0
        } else {
            val digit = localDate[index].code - '0'.code
            if (digit !in 0..9) return 0
            value = value * 10 + digit
        }
    }
    return value
}

internal fun dayKeyToDisplayString(value: Int): String {
    if (value <= 0) return ""
    val year = value / 10_000
    val month = value / 100 % 100
    val day = value % 100
    return "%02d.%02d.%04d".format(day, month, year)
}

internal fun sessionIdParts(id: ByteArray): Pair<Long, Long> {
    var high = 0L
    var low = 0L
    repeat(Long.SIZE_BYTES) { index ->
        high = high or ((id.getOrElse(index) { 0 }.toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
        low = low or ((id.getOrElse(index + Long.SIZE_BYTES) { 0 }.toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
    }
    return high to low
}

internal fun saturatedAdd(first: Long, second: Long): Long {
    if (first < 0L || second < 0L) return Long.MAX_VALUE
    return if (Long.MAX_VALUE - first < second) Long.MAX_VALUE else first + second
}

internal fun saturatedMultiply(value: Long, multiplier: Long): Long {
    if (value <= 0L || multiplier <= 0L) return 0L
    return if (value > Long.MAX_VALUE / multiplier) Long.MAX_VALUE else value * multiplier
}

private fun sessionIdHex(high: Long, low: Long): String {
    val chars = CharArray(32)
    writeHexLittleEndian(high, chars, 0)
    writeHexLittleEndian(low, chars, 16)
    return String(chars)
}

private fun writeHexLittleEndian(value: Long, target: CharArray, offset: Int) {
    repeat(Long.SIZE_BYTES) { byteIndex ->
        val byte = ((value ushr (byteIndex * Byte.SIZE_BITS)) and 0xffL).toInt()
        target[offset + byteIndex * 2] = HEX[byte ushr 4]
        target[offset + byteIndex * 2 + 1] = HEX[byte and 0x0f]
    }
}

private const val DATE_LENGTH = 10
private const val MILLIS_PER_MINUTE = 60_000L
private val HEX = "0123456789abcdef".toCharArray()
