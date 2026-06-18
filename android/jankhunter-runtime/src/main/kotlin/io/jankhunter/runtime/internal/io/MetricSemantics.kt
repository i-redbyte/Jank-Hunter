package io.jankhunter.runtime.internal.io

enum class MetricAggregationMode(val wireValue: Long) {
    UNKNOWN(0L),
    AVERAGE(1L),
    LAST(2L),
    STATE(3L),
    BOOLEAN_RATE(4L),
}

object MetricSemantics {
    fun gaugeMode(name: String?): MetricAggregationMode {
        val metric = name?.trim()?.lowercase() ?: return MetricAggregationMode.AVERAGE
        return when {
            metric in STATE_METRICS -> MetricAggregationMode.STATE
            metric in BOOLEAN_METRICS -> MetricAggregationMode.BOOLEAN_RATE
            metric.endsWith(".last_id") || metric.contains(".last.") -> MetricAggregationMode.LAST
            metric.endsWith(".last_level") -> MetricAggregationMode.LAST
            metric.endsWith(".core_count") -> MetricAggregationMode.LAST
            metric.endsWith(".max_kb") -> MetricAggregationMode.LAST
            else -> MetricAggregationMode.AVERAGE
        }
    }

    private val STATE_METRICS = setOf(
        "battery.status",
        "battery.plugged",
        "battery.health",
        "device.thermal.status",
        "process.exit.last.reason",
        "process.exit.last.importance",
        "memory.trim.last_level",
    )

    private val BOOLEAN_METRICS = setOf(
        "battery.charging",
        "device.power_save_mode",
        "device.interactive",
        "device.idle_mode",
        "network.request.connection_released",
    )
}
