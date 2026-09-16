package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeIntSource
import kotlin.math.max

internal class ProcCpuSampler(
    private val readProcessStat: () -> String?,
    private val readSystemStat: () -> String?,
    private val coreCount: RuntimeIntSource = RuntimeIntSource { Runtime.getRuntime().availableProcessors() },
) {
    private val parsedSystemTicks = LongArray(SYSTEM_TICK_RESULT_SIZE)
    private var previousProcessTicks = NO_PREVIOUS_TICKS
    private var previousTotalTicks = 0L
    private var previousIdleTicks = 0L

    fun sample(): CpuSample? {
        val processTicks = parseProcessTicks(readProcessStat()) ?: return null
        if (!parseSystemTicks(readSystemStat(), parsedSystemTicks)) return null
        val totalTicks = parsedSystemTicks[SYSTEM_TOTAL_RESULT_INDEX]
        val idleTicks = parsedSystemTicks[SYSTEM_IDLE_RESULT_INDEX]
        val lastProcessTicks = previousProcessTicks
        val lastTotalTicks = previousTotalTicks
        val lastIdleTicks = previousIdleTicks
        previousProcessTicks = processTicks
        previousTotalTicks = totalTicks
        previousIdleTicks = idleTicks
        if (lastProcessTicks == NO_PREVIOUS_TICKS) return null

        val processDelta = processTicks - lastProcessTicks
        val totalDelta = totalTicks - lastTotalTicks
        val idleDelta = idleTicks - lastIdleTicks
        if (processDelta < 0 || totalDelta <= 0 || idleDelta < 0) return null

        val cores = max(1, coreCount.getAsInt())
        val processDevicePercentX100 = scaledRatio(processDelta, PERCENT_X100, totalDelta)
        val processCorePercentX100 = scaledRatio(processDelta, PERCENT_X100 * cores.toLong(), totalDelta)
        val deviceBusyPercentX100 = scaledRatio(
            (totalDelta - idleDelta).coerceAtLeast(0L),
            PERCENT_X100,
            totalDelta,
        )
        return CpuSample(
            processDevicePercentX100 = processDevicePercentX100.coerceIn(0L, PERCENT_X100),
            processCorePercentX100 = processCorePercentX100.coerceIn(0L, PERCENT_X100 * cores.toLong()),
            deviceBusyPercentX100 = deviceBusyPercentX100.coerceIn(0L, PERCENT_X100),
            coreCount = cores,
        )
    }

    data class CpuSample(
        val processDevicePercentX100: Long,
        val processCorePercentX100: Long,
        val deviceBusyPercentX100: Long,
        val coreCount: Int,
    )

    companion object {
        private const val PERCENT_X100 = 10_000L

        fun parseProcessTicks(stat: String?): Long? {
            if (stat == null) return null
            val commandEnd = stat.lastIndexOf(") ")
            if (commandEnd < 0) return null
            var offset = commandEnd + 2
            var field = 0
            var userTicks = INVALID_TICKS
            while (offset < stat.length) {
                while (offset < stat.length && stat[offset].isWhitespace()) offset++
                if (offset == stat.length) break
                val start = offset
                while (offset < stat.length && !stat[offset].isWhitespace()) offset++
                when (field) {
                    PROCESS_UTIME_INDEX -> userTicks = parseTicks(stat, start, offset)
                    PROCESS_STIME_INDEX -> {
                        val systemTicks = parseTicks(stat, start, offset)
                        if (userTicks < 0L || systemTicks < 0L || Long.MAX_VALUE - userTicks < systemTicks) {
                            return null
                        }
                        return userTicks + systemTicks
                    }
                }
                field++
            }
            return null
        }

        private fun parseSystemTicks(stat: String?, result: LongArray): Boolean {
            if (stat == null) return false
            var offset = 0
            while (offset < stat.length && stat[offset].isWhitespace()) offset++
            if (!stat.regionMatches(offset, SYSTEM_CPU_PREFIX, 0, SYSTEM_CPU_PREFIX.length)) return false
            offset += SYSTEM_CPU_PREFIX.length
            if (offset < stat.length && !stat[offset].isWhitespace()) return false

            var field = 0
            var totalTicks = 0L
            var idleTicks = 0L
            while (offset < stat.length) {
                while (offset < stat.length && stat[offset].isWhitespace()) offset++
                if (offset == stat.length) break
                val start = offset
                while (offset < stat.length && !stat[offset].isWhitespace()) offset++
                val ticks = parseTicks(stat, start, offset)
                if (ticks < 0L) return false
                // Linux already includes guest and guest_nice in user and nice. Adding those
                // fields again inflates the denominator and under-reports process CPU usage.
                if (field < SYSTEM_GUEST_INDEX) {
                    if (Long.MAX_VALUE - totalTicks < ticks) return false
                    totalTicks += ticks
                }
                if (field == SYSTEM_IDLE_INDEX || field == SYSTEM_IOWAIT_INDEX) {
                    if (Long.MAX_VALUE - idleTicks < ticks) return false
                    idleTicks += ticks
                }
                field++
            }
            if (field == 0) return false
            result[SYSTEM_TOTAL_RESULT_INDEX] = totalTicks
            result[SYSTEM_IDLE_RESULT_INDEX] = idleTicks
            return true
        }

        private fun parseTicks(value: String, start: Int, end: Int): Long {
            if (start >= end) return INVALID_TICKS
            var result = 0L
            for (index in start until end) {
                val digit = value[index].code - '0'.code
                if (digit !in 0..9 || result > (Long.MAX_VALUE - digit) / 10L) return INVALID_TICKS
                result = result * 10L + digit
            }
            return result
        }

        private fun scaledRatio(numerator: Long, scale: Long, denominator: Long): Long {
            if (numerator == 0L) return 0L
            if (numerator <= Long.MAX_VALUE / scale) return numerator * scale / denominator
            val scaled = numerator.toDouble() * scale.toDouble() / denominator.toDouble()
            return if (scaled >= Long.MAX_VALUE.toDouble()) Long.MAX_VALUE else scaled.toLong()
        }

        private const val SYSTEM_CPU_PREFIX = "cpu"
        private const val INVALID_TICKS = -1L
        private const val PROCESS_UTIME_INDEX = 11
        private const val PROCESS_STIME_INDEX = 12
        private const val SYSTEM_IDLE_INDEX = 3
        private const val SYSTEM_IOWAIT_INDEX = 4
        private const val SYSTEM_GUEST_INDEX = 8
        private const val SYSTEM_TICK_RESULT_SIZE = 2
        private const val SYSTEM_TOTAL_RESULT_INDEX = 0
        private const val SYSTEM_IDLE_RESULT_INDEX = 1
        private const val NO_PREVIOUS_TICKS = -1L
    }
}
