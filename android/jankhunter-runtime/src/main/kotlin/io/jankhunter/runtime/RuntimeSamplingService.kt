package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.AdaptiveRuntimeSampler

internal interface RuntimeSamplingStrategy {
    fun shouldRecordMemory(nowMs: Long, pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean

    fun shouldRecordContext(
        nowMs: Long,
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        networkVpn: Boolean,
    ): Boolean
}

internal data object AlwaysRecordRuntimeSamplingStrategy : RuntimeSamplingStrategy {
    override fun shouldRecordMemory(nowMs: Long, pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean = true

    override fun shouldRecordContext(
        nowMs: Long,
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        networkVpn: Boolean,
    ): Boolean = true
}

internal class AdaptiveRuntimeSamplingStrategy(
    memoryStableIntervalMs: Long,
    contextStableIntervalMs: Long,
) : RuntimeSamplingStrategy {
    private val sampler = AdaptiveRuntimeSampler(memoryStableIntervalMs, contextStableIntervalMs)

    override fun shouldRecordMemory(nowMs: Long, pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean {
        return sampler.shouldRecordMemory(nowMs, pssKb, javaHeapKb, nativeHeapKb)
    }

    override fun shouldRecordContext(
        nowMs: Long,
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        networkVpn: Boolean,
    ): Boolean {
        return sampler.shouldRecordContext(
            nowMs,
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
    }
}

internal class RuntimeSamplingService(
    private val nowMs: () -> Long,
) {
    @Volatile
    private var strategy: RuntimeSamplingStrategy = AlwaysRecordRuntimeSamplingStrategy

    fun configure(config: JankHunterConfig) {
        strategy = if (config.adaptiveSamplingEnabled()) {
            AdaptiveRuntimeSamplingStrategy(
                config.adaptiveMemoryStableIntervalMs(),
                config.adaptiveContextStableIntervalMs(),
            )
        } else {
            AlwaysRecordRuntimeSamplingStrategy
        }
    }

    fun reset() {
        strategy = AlwaysRecordRuntimeSamplingStrategy
    }

    fun shouldRecordMemory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long): Boolean {
        return strategy.shouldRecordMemory(nowMs(), pssKb, javaHeapKb, nativeHeapKb)
    }

    fun shouldRecordContext(
        networkKind: Int,
        batteryPct: Int,
        availMemoryKb: Long,
        lowMemory: Boolean,
        networkMetered: Boolean,
        networkValidated: Boolean,
        rxBytes: Long,
        txBytes: Long,
        networkVpn: Boolean,
    ): Boolean {
        return strategy.shouldRecordContext(
            nowMs(),
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
    }
}
