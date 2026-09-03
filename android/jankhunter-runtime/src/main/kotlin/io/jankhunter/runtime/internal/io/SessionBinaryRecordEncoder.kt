package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.saturatingAdd

/** Encodes session, device, UI and retention records without owning mutable wire state. */
internal class SessionBinaryRecordEncoder(
    private val sink: BinaryEncodingSink,
) {
    fun session(
        appVersion: String?,
        build: String?,
        device: String?,
        sdkInt: Int,
        androidRelease: String?,
        securityPatch: String?,
        primaryAbi: String?,
        supportedAbis: String?,
        manufacturer: String?,
        brand: String?,
        hardware: String?,
        board: String?,
        product: String?,
        deviceRooted: Boolean,
        collectorFlags: Long,
        appForeground: Boolean,
    ) {
        require(collectorFlags and Jhlog.COLLECTOR_KNOWN_MASK.inv() == 0L)
        val payload = sink.payload()
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_APP_VERSION, appVersion))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_BUILD, build))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_DEVICE, device))
            .uvarint(nonNegative(sdkInt.toLong()))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, androidRelease))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, securityPatch))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, primaryAbi))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, supportedAbis))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, manufacturer))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, brand))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, hardware))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, board))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_GENERIC, product))
            .uvarint(nonNegative(collectorFlags))
        var attributes = foregroundFlag(appForeground)
        if (deviceRooted) attributes = attributes or BinaryLogWriter.FLAG_DEVICE_ROOTED
        sink.emitSemantic(Jhlog.TYPE_SESSION, attributes, payload, sink.producerContext())
    }

    fun deviceContext(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        batteryState: Int,
        batteryTempDeciC: Int,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        totalMemoryKb: Long,
        freeStorageKb: Long,
        totalStorageKb: Long,
        networkVpn: Boolean,
        foreground: Boolean,
    ) {
        var attributes = foregroundFlag(foreground)
        if (lowMemory) attributes = attributes or BinaryLogWriter.FLAG_CONTEXT_LOW_MEMORY
        if (networkMetered) attributes = attributes or BinaryLogWriter.FLAG_NETWORK_METERED
        if (networkValidated) attributes = attributes or BinaryLogWriter.FLAG_NETWORK_VALIDATED
        if (networkVpn) attributes = attributes or BinaryLogWriter.FLAG_NETWORK_VPN
        val payload = sink.payload()
            .uvarint(networkKind.coerceIn(0, 5).toLong())
            .uvarint(batteryPct.coerceIn(0, 100).toLong())
            .uvarint(nonNegative(availMemoryKb))
            .uvarint(nonNegative(batteryState.toLong()))
            .svarint(batteryTempDeciC.toLong())
            .uvarint(nonNegative(rxBytes))
            .uvarint(nonNegative(txBytes))
            .uvarint(nonNegative(totalMemoryKb))
            .uvarint(nonNegative(freeStorageKb))
            .uvarint(nonNegative(totalStorageKb))
        sink.emitSemantic(Jhlog.TYPE_DEVICE_CONTEXT, attributes, payload, sink.producerContext())
    }

    fun stall(
        screen: String?, owner: String?, stackHint: String?, durationMs: Long, foreground: Boolean,
    ) {
        val payload = sink.payload()
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_STACK, stackHint))
            .uvarint(nonNegative(durationMs))
        sink.emitSemantic(
            Jhlog.TYPE_STALL,
            BinaryLogWriter.FLAG_THREAD_MAIN or foregroundFlag(foreground),
            payload,
            sink.context(screen, owner),
        )
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long, foreground: Boolean) {
        val payload = sink.payload()
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(javaHeapKb))
            .uvarint(nonNegative(nativeHeapKb))
        sink.emitSemantic(Jhlog.TYPE_MEMORY, foregroundFlag(foreground), payload, sink.producerContext())
    }

    fun retained(
        screen: String?,
        owner: String?,
        className: String?,
        holder: String?,
        ageMs: Long,
        count: Long,
        foreground: Boolean,
        evidence: Long,
    ) {
        val payload = sink.payload()
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_CLASS, className))
            .symbolRef(sink.symbolId(BinaryLogWriter.DICT_OWNER, holder))
            .uvarint(nonNegative(ageMs))
            .uvarint(nonNegative(count))
            .uvarint(evidence.coerceIn(RETAINED_EVIDENCE_TIME_ONLY, RETAINED_EVIDENCE_AFTER_EXPLICIT_GC))
        sink.emitSemantic(Jhlog.TYPE_RETAINED, foregroundFlag(foreground), payload, sink.context(screen, owner))
    }

    fun uiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
        foreground: Boolean,
        flags: Long,
    ) {
        val safeWindowMs = nonNegative(windowMs).coerceAtLeast(1L)
        val safeFrameCount = nonNegative(frameCount)
        val safeJankCount = nonNegative(jankCount).coerceAtMost(safeFrameCount)
        require(source == Jhlog.UI_SOURCE_JANKSTATS || source == Jhlog.UI_SOURCE_CHOREOGRAPHER)
        require(frameDeadlineUs > 0L)
        require(frameDurationBuckets.size == Jhlog.UI_FRAME_HISTOGRAM_BUCKET_COUNT)
        require(frameDurationBuckets.all { it >= 0L })
        require(saturatingSum(frameDurationBuckets) == safeFrameCount)
        val payload = sink.payload()
            .uvarint(safeWindowMs)
            .uvarint(safeFrameCount)
            .uvarint(safeJankCount)
            .uvarint(source)
            .uvarint(frameDeadlineUs)
        frameDurationBuckets.forEach { value -> payload.uvarint(nonNegative(value)) }
        val uiFlags = flags and (BinaryLogWriter.FLAG_UI_PROBLEM or BinaryLogWriter.FLAG_UI_CLASSIFIED)
        val attributes = BinaryLogWriter.FLAG_THREAD_MAIN or foregroundFlag(foreground) or uiFlags
        sink.emitSemantic(Jhlog.TYPE_UI_WINDOW, attributes, payload, sink.producerContextWithScreen(screen))
    }

    fun processExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) {
        require(timestampUnixMs > 0L)
        val payload = sink.payload()
            .uvarint(nonNegative(reason))
            .uvarint(timestampUnixMs)
            .uvarint(nonNegative(importance))
            .uvarint(nonNegative(pssKb))
            .uvarint(nonNegative(rssKb))
            .symbolRef(sink.optionalSymbolId(BinaryLogWriter.DICT_PROCESS, processName))
        sink.emitSemantic(Jhlog.TYPE_PROCESS_EXIT, 0L, payload, sink.producerContext())
    }

    private fun saturatingSum(values: LongArray): Long {
        var total = 0L
        for (value in values) total = saturatingAdd(total, nonNegative(value))
        return total
    }

    private fun nonNegative(value: Long): Long = value.coerceAtLeast(0L)

    private fun foregroundFlag(foreground: Boolean): Long =
        if (foreground) BinaryLogWriter.FLAG_APP_FOREGROUND else 0L

    private companion object {
        const val RETAINED_EVIDENCE_TIME_ONLY = 1L
        const val RETAINED_EVIDENCE_AFTER_EXPLICIT_GC = 2L
    }
}
