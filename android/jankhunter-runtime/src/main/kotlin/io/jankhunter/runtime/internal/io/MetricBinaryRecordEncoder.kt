package io.jankhunter.runtime.internal.io

/** Encodes counter, gauge and pre-aggregated diagnostic metric records. */
internal class MetricBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    fun counter(name: String?, value: Long) {
        if (value < 0L) {
            sink.recordInvalidMetric()
            return
        }
        metric(Jhlog.TYPE_COUNTER, name, value, 1L, value, value, MetricAggregationMode.UNKNOWN)
    }

    fun stableCounter(metricId: Long, metricName: String, value: Long) {
        if (value < 0L) {
            sink.recordInvalidMetric()
            return
        }
        val metricAlias = sink.defineStableSymbol(metricId, metricName)
        val payload = sink.payload()
            .stableSymbolAlias(metricAlias)
            .uvarint(value)
            .uvarint(1L)
            .uvarint(value)
            .uvarint(value)
            .uvarint(MetricAggregationMode.UNKNOWN.wireValue)
        sink.emitSemantic(Jhlog.TYPE_COUNTER, 0L, payload, context = null)
    }

    fun gauge(name: String?, value: Long) {
        gauge(name, value, 1L, value, value, MetricAggregationMode.AVERAGE)
    }

    fun gauge(
        name: String?,
        value: Long,
        count: Long,
        sum: Long,
        max: Long,
        mode: MetricAggregationMode,
    ) {
        if (value < 0L || sum < 0L || max < 0L) {
            sink.recordInvalidMetric()
            return
        }
        metric(Jhlog.TYPE_GAUGE, name, value, count.coerceAtLeast(1L), sum, max, mode)
    }

    fun logSpam(
        screen: String?,
        owner: String?,
        operationId: Long,
        source: String?,
        level: Int,
        count: Long,
    ) {
        val payload = sink.payload()
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_LOG_SOURCE, source))
            .uvarint(nonNegative(level.toLong()))
            .uvarint(nonNegative(count))
        sink.emitSemantic(Jhlog.TYPE_LOG_SPAM, 0L, payload, sink.context(screen, owner, nonNegative(operationId)))
    }

    fun problemWindow(
        screen: String?,
        owner: String?,
        kind: String?,
        windowMs: Long,
        count: Long,
        maxMs: Long,
        foreground: Boolean,
    ) {
        val payload = sink.payload()
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_METRIC, kind))
            .uvarint(nonNegative(windowMs).coerceAtLeast(1L))
            .uvarint(nonNegative(count))
            .uvarint(nonNegative(maxMs))
        val attributes = if (foreground) BinaryLogWriter.FLAG_APP_FOREGROUND else 0L
        sink.emitSemantic(Jhlog.TYPE_PROBLEM, attributes, payload, sink.context(screen, owner))
    }

    private fun metric(
        recordType: Int,
        name: String?,
        value: Long,
        count: Long,
        sum: Long,
        max: Long,
        mode: MetricAggregationMode,
    ) {
        val normalizedCount = count.coerceAtLeast(1L)
        val normalizedSum = if (sum == 0L) value else sum
        val normalizedMax = if (max == 0L) value else max
        val payload = sink.payload()
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_METRIC, name))
            .uvarint(value)
            .uvarint(normalizedCount)
            .uvarint(normalizedSum)
            .uvarint(normalizedMax)
            .uvarint(mode.wireValue)
        sink.emitSemantic(recordType, 0L, payload, sink.producerContext())
    }

    private fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)
}
