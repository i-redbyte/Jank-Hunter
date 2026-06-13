package io.jankhunter.runtime.internal.system

import kotlin.math.max

internal class ProcCpuSampler(
    private val readProcessStat: () -> String?,
    private val readSystemStat: () -> String?,
    private val coreCount: () -> Int = { Runtime.getRuntime().availableProcessors() },
) {
    private var previous: Snapshot? = null

    fun sample(): CpuSample? {
        val processTicks = parseProcessTicks(readProcessStat()) ?: return null
        val system = parseSystemTicks(readSystemStat()) ?: return null
        val current = Snapshot(processTicks, system.totalTicks, system.idleTicks)
        val last = previous.also { previous = current } ?: return null

        val processDelta = current.processTicks - last.processTicks
        val totalDelta = current.totalTicks - last.totalTicks
        val idleDelta = current.idleTicks - last.idleTicks
        if (processDelta < 0 || totalDelta <= 0 || idleDelta < 0) return null

        val cores = max(1, coreCount())
        val processDevicePercentX100 = (processDelta * PERCENT_X100) / totalDelta
        val processCorePercentX100 = (processDelta * PERCENT_X100 * cores) / totalDelta
        val deviceBusyPercentX100 = ((totalDelta - idleDelta).coerceAtLeast(0L) * PERCENT_X100) / totalDelta
        return CpuSample(
            processDevicePercentX100 = processDevicePercentX100.coerceAtLeast(0L),
            processCorePercentX100 = processCorePercentX100.coerceAtLeast(0L),
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

    private data class Snapshot(
        val processTicks: Long,
        val totalTicks: Long,
        val idleTicks: Long,
    )

    private data class SystemTicks(
        val totalTicks: Long,
        val idleTicks: Long,
    )

    companion object {
        private const val PERCENT_X100 = 10_000L

        fun parseProcessTicks(stat: String?): Long? {
            val payload = stat?.substringAfterLast(") ", missingDelimiterValue = "")?.trim() ?: return null
            if (payload.isBlank()) return null
            val fields = payload.split(WHITESPACE)
            val userTicks = fields.getOrNull(PROCESS_UTIME_INDEX)?.toLongOrNull() ?: return null
            val systemTicks = fields.getOrNull(PROCESS_STIME_INDEX)?.toLongOrNull() ?: return null
            return userTicks + systemTicks
        }

        private fun parseSystemTicks(stat: String?): SystemTicks? {
            val fields = stat
                ?.trim()
                ?.split(WHITESPACE)
                ?.takeIf { it.isNotEmpty() && it[0] == "cpu" }
                ?: return null
            val ticks = fields.drop(1).mapNotNull { it.toLongOrNull() }
            if (ticks.isEmpty()) return null
            val idleTicks = (ticks.getOrNull(SYSTEM_IDLE_INDEX) ?: 0L) +
                (ticks.getOrNull(SYSTEM_IOWAIT_INDEX) ?: 0L)
            return SystemTicks(ticks.sum(), idleTicks)
        }

        private val WHITESPACE = Regex("\\s+")
        private const val PROCESS_UTIME_INDEX = 11
        private const val PROCESS_STIME_INDEX = 12
        private const val SYSTEM_IDLE_INDEX = 3
        private const val SYSTEM_IOWAIT_INDEX = 4
    }
}
