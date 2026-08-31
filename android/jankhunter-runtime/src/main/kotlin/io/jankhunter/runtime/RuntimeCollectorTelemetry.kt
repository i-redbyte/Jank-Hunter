package io.jankhunter.runtime

import android.app.Activity

/** Explicit telemetry boundary used by Android system collectors. */
internal interface RuntimeCollectorCallbacks {
    fun setScreen(screenName: String?)
    fun currentScreen(): String
    fun setUiVisible(visible: Boolean)
    fun isUserRelevantForSampling(): Boolean
    fun requestFlush()
    fun startScreenOpenOperation(): JankHunterOperation?
    fun watchDestroyedActivity(activity: Activity, ownerHint: String)
    fun recordCounter(name: String, value: Long)
    fun recordGauge(name: String, value: Long)
    fun recordQuality(counterId: Int, delta: Long = 1L)
    fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long)
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
    )
    fun recordUiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p95Ms: Long,
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
    )
    fun captureMainThreadStallContext(owner: String?): JankHunterContextSnapshot
    fun recordMainThreadStall(context: JankHunterContextSnapshot, stackHint: String?, durationMs: Long)
    fun recordMainThreadDispatch(durationMs: Long, thresholdMs: Long, source: String?)
    fun recordProcessExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    )
}

internal class RuntimeCollectorTelemetry(
    private val contexts: ContextTracker,
    private val access: RuntimeTelemetryAccess,
    private val contextTelemetry: RuntimeContextTelemetry,
    private val operationTelemetry: RuntimeOperationTelemetry,
    private val retentionTelemetry: RuntimeRetentionTelemetry,
    private val systemTelemetry: RuntimeSystemTelemetry,
    private val asyncTelemetry: RuntimeAsyncTelemetry,
    private val flushRequest: () -> Unit,
) : RuntimeCollectorCallbacks {
    override fun setScreen(screenName: String?) = contextTelemetry.setScreen(screenName)

    override fun currentScreen(): String = contexts.currentScreen()

    override fun setUiVisible(visible: Boolean) = contextTelemetry.setUiVisible(visible)

    override fun isUserRelevantForSampling(): Boolean = access.isUserRelevantForSampling()

    override fun requestFlush() = flushRequest()

    override fun startScreenOpenOperation(): JankHunterOperation? {
        return operationTelemetry.start(
            SCREEN_OPEN_OPERATION,
            JankHunterOperationKind.SCREEN,
            budgetMs = 0L,
            attributes = JankHunterOperationAttributes.EMPTY,
        ).takeIf { it.id != 0L }
    }

    override fun watchDestroyedActivity(activity: Activity, ownerHint: String) {
        retentionTelemetry.watchActivity(activity, ownerHint)
    }

    override fun recordCounter(name: String, value: Long) = systemTelemetry.recordCounter(name, value)

    override fun recordGauge(name: String, value: Long) = systemTelemetry.recordGauge(name, value)

    override fun recordQuality(counterId: Int, delta: Long) = systemTelemetry.recordQuality(counterId, delta)

    override fun recordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        systemTelemetry.recordMemory(pssKb, javaHeapKb, nativeHeapKb)
    }

    override fun recordContext(
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
    ) = systemTelemetry.recordContext(
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
    )

    override fun recordUiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p95Ms: Long,
        source: Long,
        frameDeadlineUs: Long,
        frameDurationBuckets: LongArray,
    ) = systemTelemetry.recordUiWindow(
        screen,
        windowMs,
        frameCount,
        jankCount,
        p95Ms,
        source,
        frameDeadlineUs,
        frameDurationBuckets,
    )

    override fun captureMainThreadStallContext(owner: String?): JankHunterContextSnapshot {
        return contextTelemetry.captureMainThreadStallContext(owner)
    }

    override fun recordMainThreadStall(
        context: JankHunterContextSnapshot,
        stackHint: String?,
        durationMs: Long,
    ) = contextTelemetry.recordMainThreadStall(context, stackHint, durationMs)

    override fun recordMainThreadDispatch(durationMs: Long, thresholdMs: Long, source: String?) {
        asyncTelemetry.recordMainThreadDispatch(durationMs, thresholdMs, source)
    }

    override fun recordProcessExit(
        reason: Long,
        timestampUnixMs: Long,
        importance: Long,
        pssKb: Long,
        rssKb: Long,
        processName: String?,
    ) = systemTelemetry.recordProcessExit(reason, timestampUnixMs, importance, pssKb, rssKb, processName)

    private companion object {
        const val SCREEN_OPEN_OPERATION = "screen.open"
    }
}
