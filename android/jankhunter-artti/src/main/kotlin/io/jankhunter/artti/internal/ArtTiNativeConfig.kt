package io.jankhunter.artti.internal

import java.nio.ByteBuffer
import java.nio.ByteOrder

internal data class ArtTiNativeConfig(
    val profile: Int = PROFILE_CAUSAL,
    val transportCapacity: Int = 4096,
    val maxTrackedThreads: Int = 512,
    val maxOpenContentions: Int = 1024,
    val maxStackDepth: Int = 64,
    val maxStackDefinitions: Int = 1024,
    val maxMethodDefinitions: Int = 4096,
    val minStackTriggerIntervalMs: Int = 250,
    val maxStackSamplesPerMinute: Int = 120,
    val drainBatchSize: Int = 256,
    val minContentionDurationNs: Long = 8_000_000L,
    val configHash: Long = 0L,
    val requestedCapabilities: Long = 0L,
) {
    fun validate(): ArtTiNativeStatus {
        if (profile !in PROFILE_OFF..PROFILE_CUSTOM) return ArtTiNativeStatus.INVALID_ARGUMENT
        if (requestedCapabilities and KNOWN_CAPABILITY_BITS.inv() != 0L) {
            return ArtTiNativeStatus.INVALID_ARGUMENT
        }
        if (!transportCapacity.isPowerOfTwo() || transportCapacity !in 2..MAX_TRANSPORT_CAPACITY) {
            return ArtTiNativeStatus.INVALID_ARGUMENT
        }
        if (maxTrackedThreads !in 1..MAX_TRACKED_THREADS ||
            maxOpenContentions !in 1..MAX_OPEN_CONTENTIONS ||
            maxStackDepth !in 1..MAX_STACK_DEPTH ||
            maxStackDefinitions !in 1..MAX_STACK_DEFINITIONS ||
            maxMethodDefinitions !in 1..MAX_METHOD_DEFINITIONS ||
            minStackTriggerIntervalMs !in 0..MAX_TRIGGER_INTERVAL_MS ||
            maxStackSamplesPerMinute !in 1..MAX_STACK_SAMPLES_PER_MINUTE ||
            drainBatchSize !in 1..transportCapacity ||
            minContentionDurationNs < 0L
        ) {
            return ArtTiNativeStatus.INVALID_ARGUMENT
        }
        if (requestedCapabilities and (CAPABILITY_STACK_TRACE or CAPABILITY_THREAD_METADATA or CAPABILITY_LINUX_TID) != 0L &&
            requestedCapabilities and CAPABILITY_THREAD_EVENTS == 0L
        ) {
            return ArtTiNativeStatus.INVALID_ARGUMENT
        }
        return ArtTiNativeStatus.OK
    }

    fun encodeDirect(): ByteBuffer {
        check(validate() == ArtTiNativeStatus.OK) { "Invalid ART TI native config" }
        return ByteBuffer.allocateDirect(ArtTiNativeProtocol.CONFIG_WIRE_SIZE)
            .order(ByteOrder.LITTLE_ENDIAN)
            .apply {
                putInt(ArtTiNativeProtocol.CONFIG_WIRE_SIZE)
                putInt(CONFIG_SCHEMA_VERSION)
                putInt(profile)
                putInt(transportCapacity)
                putInt(maxTrackedThreads)
                putInt(maxOpenContentions)
                putInt(maxStackDepth)
                putInt(drainBatchSize)
                putLong(minContentionDurationNs)
                putLong(configHash)
                putLong(requestedCapabilities)
                putInt(maxStackDefinitions)
                putInt(maxMethodDefinitions)
                putInt(minStackTriggerIntervalMs)
                putInt(maxStackSamplesPerMinute)
                flip()
            }
    }

    private fun Int.isPowerOfTwo(): Boolean = this > 0 && this and (this - 1) == 0

    companion object {
        const val PROFILE_OFF = 0
        const val PROFILE_LIGHT = 1
        const val PROFILE_CAUSAL = 2
        const val PROFILE_DEEP = 3
        const val PROFILE_CUSTOM = 4
        const val CAPABILITY_GC_EVENTS = 1L shl 0
        const val CAPABILITY_THREAD_EVENTS = 1L shl 1
        const val CAPABILITY_MONITOR_EVENTS = 1L shl 2
        const val CAPABILITY_STACK_TRACE = 1L shl 3
        const val CAPABILITY_THREAD_METADATA = 1L shl 4
        const val CAPABILITY_LINUX_TID = 1L shl 5
        private const val KNOWN_CAPABILITY_BITS = CAPABILITY_GC_EVENTS or CAPABILITY_THREAD_EVENTS or
            CAPABILITY_MONITOR_EVENTS or CAPABILITY_STACK_TRACE or CAPABILITY_THREAD_METADATA or CAPABILITY_LINUX_TID
        private const val CONFIG_SCHEMA_VERSION = 1
        private const val MAX_TRANSPORT_CAPACITY = 65_536
        private const val MAX_TRACKED_THREADS = 16_384
        private const val MAX_OPEN_CONTENTIONS = 65_536
        private const val MAX_STACK_DEPTH = 256
        private const val MAX_STACK_DEFINITIONS = 65_536
        private const val MAX_METHOD_DEFINITIONS = 262_144
        private const val MAX_TRIGGER_INTERVAL_MS = 60_000
        private const val MAX_STACK_SAMPLES_PER_MINUTE = 10_000
    }
}
