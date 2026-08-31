package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.UiWindowClassifier

internal class RuntimeSystemTelemetry(
    private val contexts: ContextTracker,
    private val access: RuntimeTelemetryAccess,
    private val metrics: RuntimeMetricsService,
    private val sampling: RuntimeSamplingService,
    private val hookEvents: RuntimeHookEventTransport,
) {
    fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        if (!sampling.shouldRecordMemory(pssKb, javaHeapKb, nativeHeapKb)) return
        access.ensureContextRecorded()
        access.writer?.memory(pssKb, javaHeapKb, nativeHeapKb, foreground = access.isUiVisible())
    }

    fun recordContext(
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
    ) {
        if (!sampling.shouldRecordContext(
                networkKind,
                batteryPct,
                availMemoryKb,
                lowMemory,
                networkMetered,
                networkValidated,
                rxBytes,
                txBytes,
                networkVpn,
            )
        ) {
            return
        }
        access.ensureContextRecorded()
        access.writer?.context(
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
            foreground = access.isUiVisible(),
        )
    }

    fun recordUiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p95Ms: Long,
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
    ) {
        val attributedScreen = firstContextValue(screen, contexts.currentScreen())
        access.ensureContextRecorded(screenOverride = attributedScreen)
        val p95ThresholdMs = access.config?.uiWindowP95ThresholdMs() ?: DEFAULT_UI_WINDOW_P95_THRESHOLD_MS
        val flags = UiWindowClassifier.flags(jankCount, p95Ms, p95ThresholdMs)
        access.writer?.uiWindow(
            attributedScreen,
            windowMs,
            frameCount,
            jankCount,
            source,
            frameDeadlineUs,
            frameDurationBuckets,
            foreground = access.isUiVisible(),
            flags = flags,
        )
    }

    fun recordCounter(name: String?, value: Long) {
        RuntimeHookGuard.run { metrics.recordCounter(name, value) }
    }

    fun recordGauge(name: String?, value: Long) {
        RuntimeHookGuard.run { metrics.recordGauge(name, value) }
    }

    fun recordQuality(counterId: Int, delta: Long) {
        access.writer?.recordQuality(counterId, delta)
    }

    fun recordProcessExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) {
        access.writer?.processExit(
            reason,
            timestampUnixMs,
            importance,
            pssKb,
            rssKb,
            access.config?.redactProcessName(processName) ?: processName,
        )
    }

    fun recordLogSpam(ownerName: String?, source: String?, level: Int) {
        RuntimeHookGuard.run {
            if (!access.isActive()) return@run
            hookEvents.recordLogSpam(
                screen = contexts.currentScreenOrNull(),
                owner = ownerName?.takeIf { it.isNotBlank() } ?: contexts.ownerOrNull(),
                source = source,
                level = level,
                operationId = contexts.currentOperationId(),
            )
        }
    }

    private companion object {
        const val DEFAULT_UI_WINDOW_P95_THRESHOLD_MS = 32L
    }
}
