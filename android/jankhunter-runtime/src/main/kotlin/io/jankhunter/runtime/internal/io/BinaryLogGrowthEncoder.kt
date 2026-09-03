package io.jankhunter.runtime.internal.io

/** Owns log-growth checkpoints, delta state and their control-record encoding. */
internal class BinaryLogGrowthEncoder(
    private val sink: BinaryEncodingSink,
    private val container: BinaryLogContainer,
    private val quality: LogQualityCounters,
    private val binding: LogGrowthSessionBinding?,
) {
    private var previousHistory = ByteArray(INITIAL_STATE_BYTES)
    private var previousHistorySize = 0
    private var previousLive = ByteArray(INITIAL_STATE_BYTES)
    private var previousLiveSize = 0
    private val payload = BinaryPayload(256)

    val enabled: Boolean
        get() = binding != null

    fun begin(fileHeader: BinaryLogFileHeader, storageBudgetExhausted: Boolean) {
        val active = binding ?: return
        try {
            if (fileHeader.segmentIndex == 0L) {
                val started = active.manager.beginSession(
                    sessionId = fileHeader.sessionId,
                    localDate = active.localDate,
                    startedAtMs = fileHeader.segmentStartUnixMs,
                    configuredLimitBytes = active.configuredLimitBytes,
                    stats = stats(storageBudgetExhausted),
                )
                emit(Jhlog.LOG_GROWTH_HISTORY, started.history)
                emit(Jhlog.LOG_GROWTH_LIVE, started.live)
            } else {
                active.manager.checkpoint(stats(storageBudgetExhausted))?.let { live ->
                    emit(Jhlog.LOG_GROWTH_LIVE, live)
                }
            }
        } catch (error: Throwable) {
            if (error is VirtualMachineError || error is ThreadDeath) throw error
        }
    }

    fun checkpoint(storageBudgetExhausted: Boolean): Boolean {
        val active = binding ?: return false
        val live = active.manager.checkpoint(stats(storageBudgetExhausted)) ?: return false
        emit(Jhlog.LOG_GROWTH_LIVE, live)
        return true
    }

    fun complete(storageBudgetExhausted: Boolean): Boolean {
        val active = binding ?: return false
        val live = active.manager.complete(stats(storageBudgetExhausted)) ?: return false
        emit(Jhlog.LOG_GROWTH_LIVE, live)
        return true
    }

    fun stats(storageBudgetExhausted: Boolean): LogContainerStats {
        val current = container.stats()
        val base = binding?.baseStats ?: return current
        return LogContainerStats(
            retainedBytes = saturatedAdd(base.retainedBytes, current.retainedBytes),
            generatedBytes = saturatedAdd(base.generatedBytes, current.generatedBytes),
            limitReachedCount = maxOf(base.limitReachedCount, if (storageBudgetExhausted) 1L else 0L),
            segmentRotationCount = saturatedAdd(base.segmentRotationCount, current.segmentRotationCount),
            archiveEvictedBytes = maxOf(
                base.archiveEvictedBytes,
                current.archiveEvictedBytes,
                quality.value(QualityCounterId.ARCHIVE_EVICTED_BYTES_TOTAL),
            ),
        )
    }

    private fun emit(kind: Long, raw: ByteArray) {
        val previous = if (kind == Jhlog.LOG_GROWTH_HISTORY) previousHistory else previousLive
        val previousSize = if (kind == Jhlog.LOG_GROWTH_HISTORY) previousHistorySize else previousLiveSize
        var prefix = 0
        val prefixLimit = minOf(previousSize, raw.size)
        while (prefix < prefixLimit && previous[prefix] == raw[prefix]) prefix++
        var suffix = 0
        while (
            suffix < previousSize - prefix &&
            suffix < raw.size - prefix &&
            previous[previousSize - suffix - 1] == raw[raw.size - suffix - 1]
        ) {
            suffix++
        }
        val middleSize = raw.size - prefix - suffix
        val deltaBytes = PackedLongs.uvarintSize(prefix.toLong()) +
            PackedLongs.uvarintSize(suffix.toLong()) + middleSize
        val useDelta = previousSize > 0 && deltaBytes < raw.size
        val encoded = payload.clear()
            .uvarint(kind)
            .uvarint(if (useDelta) CONTROL_DELTA_PREFIX_SUFFIX else CONTROL_DELTA_FULL)
            .uvarint(raw.size.toLong())
        if (useDelta) {
            encoded
                .uvarint(prefix.toLong())
                .uvarint(suffix.toLong())
                .bytes(raw, prefix, middleSize)
        } else {
            encoded.bytes(raw)
        }
        sink.emitControl(Jhlog.TYPE_LOG_GROWTH, encoded)
        if (kind == Jhlog.LOG_GROWTH_HISTORY) {
            if (previousHistory.size < raw.size) previousHistory = ByteArray(raw.size)
            raw.copyInto(previousHistory)
            previousHistorySize = raw.size
        } else {
            if (previousLive.size < raw.size) previousLive = ByteArray(raw.size)
            raw.copyInto(previousLive)
            previousLiveSize = raw.size
        }
    }

    private companion object {
        const val INITIAL_STATE_BYTES = 256
        const val CONTROL_DELTA_FULL = 0L
        const val CONTROL_DELTA_PREFIX_SUFFIX = 1L
    }
}
