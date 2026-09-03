package io.jankhunter.runtime.internal.io

internal class PendingSessionEvent(
    producerContext: LogEventContext?,
    private val appVersion: String?,
    private val build: String?,
    private val device: String?,
    private val sdkInt: Int,
    private val androidRelease: String?,
    private val securityPatch: String?,
    private val primaryAbi: String?,
    private val supportedAbis: String?,
    private val manufacturer: String?,
    private val brand: String?,
    private val hardware: String?,
    private val board: String?,
    private val product: String?,
    private val deviceRooted: Boolean,
    private val collectorFlags: Long,
) : PendingLogEvent(Jhlog.TYPE_SESSION, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.session(
            appVersion,
            build,
            device,
            sdkInt,
            androidRelease,
            securityPatch,
            primaryAbi,
            supportedAbis,
            manufacturer,
            brand,
            hardware,
            board,
            product,
            deviceRooted,
            collectorFlags,
            appForeground = false,
        )
    }
}

internal class PendingDeviceContextEvent(
    producerContext: LogEventContext?,
    private val networkKind: Int,
    private val batteryPct: Int,
    private val availMemoryKb: Long,
    private val batteryState: Int,
    private val batteryTempDeciC: Int,
    private val lowMemory: Boolean,
    private val networkMetered: Boolean,
    private val networkValidated: Boolean,
    private val rxBytes: Long,
    private val txBytes: Long,
    private val totalMemoryKb: Long,
    private val freeStorageKb: Long,
    private val totalStorageKb: Long,
    private val networkVpn: Boolean,
    private val foreground: Boolean,
) : PendingLogEvent(Jhlog.TYPE_DEVICE_CONTEXT, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.context(
            networkKind,
            batteryPct,
            availMemoryKb,
            batteryState,
            batteryTempDeciC,
            lowMemory,
            networkMetered,
            networkValidated,
            rxBytes,
            txBytes,
            totalMemoryKb,
            freeStorageKb,
            totalStorageKb,
            networkVpn,
            foreground,
        )
    }
}

internal class PendingUiWindowEvent(
    producerContext: LogEventContext?,
    private val screen: String?,
    private val windowMs: Long,
    private val frameCount: Long,
    private val jankCount: Long,
    private val source: Long,
    private val frameDeadlineUs: Long,
    private val frameDurationBuckets: LongArray,
    private val foreground: Boolean,
    private val flags: Long,
) : PendingLogEvent(Jhlog.TYPE_UI_WINDOW, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.uiWindow(
            screen,
            windowMs,
            frameCount,
            jankCount,
            source,
            frameDeadlineUs,
            frameDurationBuckets,
            foreground,
            flags,
        )
    }
}

internal class PendingProcessExitEvent(
    producerContext: LogEventContext?,
    private val reason: Long,
    private val timestampUnixMs: Long,
    private val importance: Long,
    private val pssKb: Long,
    private val rssKb: Long,
    private val processName: String?,
) : PendingLogEvent(Jhlog.TYPE_PROCESS_EXIT, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.processExit(reason, timestampUnixMs, importance, pssKb, rssKb, processName)
    }
}

internal class PendingProcessStateEvent(
    producerContext: LogEventContext?,
    private val uiVisibility: Long,
    private val processImportance: Long,
    private val androidImportance: Long,
    private val reason: Long,
) : PendingLogEvent(Jhlog.TYPE_PROCESS_STATE, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.processState(uiVisibility, processImportance, androidImportance, reason)
    }
}

internal class PendingStallEvent(
    producerContext: LogEventContext?,
    private val screen: String?,
    private val owner: String?,
    private val stackHint: String?,
    private val durationMs: Long,
    private val foreground: Boolean,
) : PendingLogEvent(Jhlog.TYPE_STALL, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.stall(screen, owner, stackHint, durationMs, foreground)
    }
}

internal class PendingMemoryEvent(
    producerContext: LogEventContext?,
    private val pssKb: Long,
    private val javaHeapKb: Long,
    private val nativeHeapKb: Long,
    private val foreground: Boolean,
) : PendingLogEvent(Jhlog.TYPE_MEMORY, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.memory(pssKb, javaHeapKb, nativeHeapKb, foreground)
    }
}

internal class PendingRetainedEvent(
    producerContext: LogEventContext?,
    private val screen: String?,
    private val owner: String?,
    private val className: String?,
    private val holder: String?,
    private val ageMs: Long,
    private val count: Long,
    private val foreground: Boolean,
    private val evidence: Long,
) : PendingLogEvent(Jhlog.TYPE_RETAINED, producerContext) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.retained(screen, owner, className, holder, ageMs, count, foreground, evidence)
    }
}
