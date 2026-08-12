package io.jankhunter.gradle

import org.gradle.api.GradleException

internal data class EffectiveArtTiConfig(
    val mode: ArtTiMode,
    val garbageCollectionEnabled: Boolean,
    val threadLifecycleEnabled: Boolean,
    val maxTrackedThreads: Int,
    val monitorContentionEnabled: Boolean,
    val minContentionDurationMs: Long,
    val maxOpenContentions: Int,
    val stackSamplingEnabled: Boolean,
    val maxStackDepth: Int,
    val onMainThreadStall: Boolean,
    val onLongContention: Boolean,
    val minTriggerIntervalMs: Long,
    val maxSamplesPerMinute: Int,
    val maxStackDefinitions: Int,
    val maxMethodDefinitions: Int,
    val transportCapacity: Int,
    val drainBatchSize: Int,
    val overflowPolicy: ArtTiOverflowPolicy,
    val configHash: Long,
) {
    val enabled: Boolean get() = mode != ArtTiMode.OFF

    val requestedCapabilities: Long
        get() {
            var result = 0L
            if (garbageCollectionEnabled) result = result or CAPABILITY_GC_EVENTS
            if (threadLifecycleEnabled) {
                result = result or CAPABILITY_THREAD_EVENTS or CAPABILITY_THREAD_METADATA or CAPABILITY_LINUX_TID
            }
            if (monitorContentionEnabled) result = result or CAPABILITY_MONITOR_EVENTS
            if (stackSamplingEnabled) result = result or CAPABILITY_STACK_TRACE
            return result
        }

    fun nativeAgentOptions(): String {
        return listOf(
            "v=1",
            "profile=${mode.nativeProfileId()}",
            "transport=$transportCapacity",
            "threads=$maxTrackedThreads",
            "contentions=$maxOpenContentions",
            "depth=$maxStackDepth",
            "stackdefs=$maxStackDefinitions",
            "methoddefs=$maxMethodDefinitions",
            "batch=$drainBatchSize",
            "mincontentionns=${minContentionDurationMs * NANOS_PER_MS}",
            "hash=0x${java.lang.Long.toUnsignedString(configHash, 16)}",
            "cap=0x${java.lang.Long.toUnsignedString(requestedCapabilities, 16)}",
        ).joinToString(";")
    }

    fun triggerPolicy(): String {
        return listOf(
            "v=1",
            "main=${onMainThreadStall.asInt()}",
            "long=${onLongContention.asInt()}",
            "minms=$minTriggerIntervalMs",
            "maxpm=$maxSamplesPerMinute",
            "drainms=$DEFAULT_DRAIN_INTERVAL_MS",
        ).joinToString(";")
    }

    companion object {
        private const val NANOS_PER_MS = 1_000_000L
        private const val DEFAULT_DRAIN_INTERVAL_MS = 50L
        private const val CAPABILITY_GC_EVENTS = 1L shl 0
        private const val CAPABILITY_THREAD_EVENTS = 1L shl 1
        private const val CAPABILITY_MONITOR_EVENTS = 1L shl 2
        private const val CAPABILITY_STACK_TRACE = 1L shl 3
        private const val CAPABILITY_THREAD_METADATA = 1L shl 4
        private const val CAPABILITY_LINUX_TID = 1L shl 5
    }
}

internal object EffectiveArtTiConfigResolver {
    fun resolve(dsl: JankHunterExtension.ArtTi): EffectiveArtTiConfig {
        val mode = dsl.mode.getOrElse(ArtTiMode.OFF)
        val preset = preset(mode)
        if (mode == ArtTiMode.CUSTOM) requireCustomProperties(dsl)
        val candidate = preset.copy(
            garbageCollectionEnabled = dsl.garbageCollection.enabled.orNull ?: preset.garbageCollectionEnabled,
            threadLifecycleEnabled = dsl.threads.lifecycle.orNull ?: preset.threadLifecycleEnabled,
            maxTrackedThreads = dsl.threads.maxTrackedThreads.orNull ?: preset.maxTrackedThreads,
            monitorContentionEnabled = dsl.monitorContention.enabled.orNull ?: preset.monitorContentionEnabled,
            minContentionDurationMs = dsl.monitorContention.minDurationMs.orNull
                ?: preset.minContentionDurationMs,
            maxOpenContentions = dsl.monitorContention.maxOpenIntervals.orNull ?: preset.maxOpenContentions,
            stackSamplingEnabled = dsl.stackSampling.enabled.orNull ?: preset.stackSamplingEnabled,
            maxStackDepth = dsl.stackSampling.maxDepth.orNull ?: preset.maxStackDepth,
            onMainThreadStall = dsl.stackSampling.onMainThreadStall.orNull ?: preset.onMainThreadStall,
            onLongContention = dsl.stackSampling.onLongContention.orNull ?: preset.onLongContention,
            minTriggerIntervalMs = dsl.stackSampling.minTriggerIntervalMs.orNull
                ?: preset.minTriggerIntervalMs,
            maxSamplesPerMinute = dsl.stackSampling.maxSamplesPerMinute.orNull
                ?: preset.maxSamplesPerMinute,
            maxStackDefinitions = dsl.stackSampling.maxStackDefinitions.orNull ?: preset.maxStackDefinitions,
            maxMethodDefinitions = dsl.stackSampling.maxMethodDefinitions.orNull ?: preset.maxMethodDefinitions,
            transportCapacity = dsl.transport.capacity.orNull ?: preset.transportCapacity,
            drainBatchSize = dsl.transport.drainBatchSize.orNull ?: preset.drainBatchSize,
            overflowPolicy = dsl.transport.overflowPolicy.orNull ?: preset.overflowPolicy,
        )
        validate(candidate)
        return candidate.copy(configHash = hash(candidate))
    }

    private fun preset(mode: ArtTiMode): EffectiveArtTiConfig {
        return when (mode) {
            ArtTiMode.OFF -> base(mode).copy(
                maxTrackedThreads = 1,
                maxOpenContentions = 1,
                maxStackDepth = 1,
                maxStackDefinitions = 1,
                maxMethodDefinitions = 1,
                transportCapacity = 2,
                drainBatchSize = 1,
            )
            ArtTiMode.LIGHT -> base(mode).copy(
                garbageCollectionEnabled = true,
                threadLifecycleEnabled = true,
            )
            ArtTiMode.CAUSAL -> base(mode).copy(
                garbageCollectionEnabled = true,
                threadLifecycleEnabled = true,
                monitorContentionEnabled = true,
                stackSamplingEnabled = true,
                onMainThreadStall = true,
                onLongContention = true,
            )
            ArtTiMode.DEEP -> base(mode).copy(
                garbageCollectionEnabled = true,
                threadLifecycleEnabled = true,
                maxTrackedThreads = 1024,
                monitorContentionEnabled = true,
                maxOpenContentions = 4096,
                stackSamplingEnabled = true,
                maxStackDepth = 128,
                onMainThreadStall = true,
                onLongContention = true,
                minTriggerIntervalMs = 50L,
                maxSamplesPerMinute = 600,
                maxStackDefinitions = 4096,
                maxMethodDefinitions = 16_384,
                transportCapacity = 16_384,
                drainBatchSize = 512,
            )
            ArtTiMode.CUSTOM -> base(mode)
        }
    }

    private fun base(mode: ArtTiMode) = EffectiveArtTiConfig(
        mode = mode,
        garbageCollectionEnabled = false,
        threadLifecycleEnabled = false,
        maxTrackedThreads = 512,
        monitorContentionEnabled = false,
        minContentionDurationMs = 8L,
        maxOpenContentions = 1024,
        stackSamplingEnabled = false,
        maxStackDepth = 64,
        onMainThreadStall = false,
        onLongContention = false,
        minTriggerIntervalMs = 250L,
        maxSamplesPerMinute = 120,
        maxStackDefinitions = 1024,
        maxMethodDefinitions = 4096,
        transportCapacity = 4096,
        drainBatchSize = 256,
        overflowPolicy = ArtTiOverflowPolicy.DROP_AND_COUNT,
        configHash = 0L,
    )

    private fun requireCustomProperties(dsl: JankHunterExtension.ArtTi) {
        val missing = buildList {
            required("garbageCollection.enabled", dsl.garbageCollection.enabled.isPresent)
            required("threads.lifecycle", dsl.threads.lifecycle.isPresent)
            required("threads.maxTrackedThreads", dsl.threads.maxTrackedThreads.isPresent)
            required("monitorContention.enabled", dsl.monitorContention.enabled.isPresent)
            required("monitorContention.minDurationMs", dsl.monitorContention.minDurationMs.isPresent)
            required("monitorContention.maxOpenIntervals", dsl.monitorContention.maxOpenIntervals.isPresent)
            required("stackSampling.enabled", dsl.stackSampling.enabled.isPresent)
            required("stackSampling.maxDepth", dsl.stackSampling.maxDepth.isPresent)
            required("stackSampling.onMainThreadStall", dsl.stackSampling.onMainThreadStall.isPresent)
            required("stackSampling.onLongContention", dsl.stackSampling.onLongContention.isPresent)
            required("stackSampling.minTriggerIntervalMs", dsl.stackSampling.minTriggerIntervalMs.isPresent)
            required("stackSampling.maxSamplesPerMinute", dsl.stackSampling.maxSamplesPerMinute.isPresent)
            required("stackSampling.maxStackDefinitions", dsl.stackSampling.maxStackDefinitions.isPresent)
            required("stackSampling.maxMethodDefinitions", dsl.stackSampling.maxMethodDefinitions.isPresent)
            required("transport.capacity", dsl.transport.capacity.isPresent)
            required("transport.drainBatchSize", dsl.transport.drainBatchSize.isPresent)
            required("transport.overflowPolicy", dsl.transport.overflowPolicy.isPresent)
        }
        if (missing.isNotEmpty()) {
            throw GradleException("jankHunter.artTi CUSTOM requires explicit: ${missing.joinToString()}")
        }
    }

    private fun MutableList<String>.required(name: String, present: Boolean) {
        if (!present) add(name)
    }

    private fun validate(config: EffectiveArtTiConfig) {
        if (config.mode == ArtTiMode.OFF) return
        requireRange("threads.maxTrackedThreads", config.maxTrackedThreads, 1, 16_384)
        requireRange("monitorContention.minDurationMs", config.minContentionDurationMs, 0L, 60_000L)
        requireRange("monitorContention.maxOpenIntervals", config.maxOpenContentions, 1, 65_536)
        requireRange("stackSampling.maxDepth", config.maxStackDepth, 1, 256)
        requireRange("stackSampling.minTriggerIntervalMs", config.minTriggerIntervalMs, 0L, 60_000L)
        requireRange("stackSampling.maxSamplesPerMinute", config.maxSamplesPerMinute, 1, 10_000)
        requireRange("stackSampling.maxStackDefinitions", config.maxStackDefinitions, 1, 65_536)
        requireRange("stackSampling.maxMethodDefinitions", config.maxMethodDefinitions, 1, 262_144)
        requireRange("transport.capacity", config.transportCapacity, 2, 65_536)
        requireRange("transport.drainBatchSize", config.drainBatchSize, 1, config.transportCapacity)
        if (config.transportCapacity and (config.transportCapacity - 1) != 0) {
            invalid("transport.capacity must be a power of two")
        }
        if (config.drainBatchSize > MAX_DRAIN_RECORDS) {
            invalid("transport.drainBatchSize must be <= $MAX_DRAIN_RECORDS")
        }
        if ((config.stackSamplingEnabled || config.monitorContentionEnabled) && !config.threadLifecycleEnabled) {
            invalid("threads.lifecycle must be enabled for contention or stack evidence")
        }
        if (!config.stackSamplingEnabled && (config.onMainThreadStall || config.onLongContention)) {
            invalid("stack triggers require stackSampling.enabled=true")
        }
        if (config.mode == ArtTiMode.LIGHT && config.stackSamplingEnabled) {
            invalid("LIGHT does not enable stack walking; use CAUSAL, DEEP, or CUSTOM")
        }
        if (config.overflowPolicy != ArtTiOverflowPolicy.DROP_AND_COUNT) {
            invalid("only DROP_AND_COUNT is supported in ART TI V1")
        }
    }

    private fun hash(config: EffectiveArtTiConfig): Long {
        val canonical = listOf(
            "schema=1",
            "mode=${config.mode.name}",
            "gc=${config.garbageCollectionEnabled.asInt()}",
            "threads=${config.threadLifecycleEnabled.asInt()}",
            "maxThreads=${config.maxTrackedThreads}",
            "monitor=${config.monitorContentionEnabled.asInt()}",
            "minContentionMs=${config.minContentionDurationMs}",
            "maxContentions=${config.maxOpenContentions}",
            "stack=${config.stackSamplingEnabled.asInt()}",
            "depth=${config.maxStackDepth}",
            "main=${config.onMainThreadStall.asInt()}",
            "long=${config.onLongContention.asInt()}",
            "minTriggerMs=${config.minTriggerIntervalMs}",
            "maxSamples=${config.maxSamplesPerMinute}",
            "stackDefinitions=${config.maxStackDefinitions}",
            "methodDefinitions=${config.maxMethodDefinitions}",
            "transport=${config.transportCapacity}",
            "batch=${config.drainBatchSize}",
            "overflow=${config.overflowPolicy.name}",
        ).joinToString(";")
        var result = FNV_OFFSET_BASIS
        canonical.encodeToByteArray().forEach { byte ->
            result = result xor (byte.toLong() and 0xffL)
            result *= FNV_PRIME
        }
        return if (result == 0L) 1L else result
    }

    private fun requireRange(name: String, value: Int, minimum: Int, maximum: Int) {
        if (value !in minimum..maximum) invalid("$name must be in $minimum..$maximum, but was $value")
    }

    private fun requireRange(name: String, value: Long, minimum: Long, maximum: Long) {
        if (value !in minimum..maximum) invalid("$name must be in $minimum..$maximum, but was $value")
    }

    private fun invalid(message: String): Nothing = throw GradleException("Invalid jankHunter.artTi: $message")

    private const val MAX_DRAIN_RECORDS = 2_048
    private const val FNV_OFFSET_BASIS = -3750763034362895579L
    private const val FNV_PRIME = 1099511628211L
}

private fun Boolean.asInt(): Int = if (this) 1 else 0

private fun ArtTiMode.nativeProfileId(): Int = when (this) {
    ArtTiMode.OFF -> 0
    ArtTiMode.LIGHT -> 1
    ArtTiMode.CAUSAL -> 2
    ArtTiMode.DEEP -> 3
    ArtTiMode.CUSTOM -> 4
}
