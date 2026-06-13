package io.jankhunter.runtime

import java.io.File

class JankHunterConfig private constructor(builder: Builder) {
    private val enabled = builder.enabled
    private val autoStartCollectors = builder.autoStartCollectors
    private val mainThreadStallThresholdMs = builder.mainThreadStallThresholdMs
    private val memorySampleIntervalMs = builder.memorySampleIntervalMs
    private val fpsMonitorEnabled = builder.fpsMonitorEnabled
    private val fpsWindowMs = builder.fpsWindowMs
    private val jankFrameThresholdMs = builder.jankFrameThresholdMs
    private val maxQueueSize = builder.maxQueueSize
    private val maxLogBytes = builder.maxLogBytes
    private val flushIntervalMs = builder.flushIntervalMs
    private val logDirectory = builder.logDirectory

    fun enabled(): Boolean = enabled

    fun autoStartCollectors(): Boolean = autoStartCollectors

    fun mainThreadStallThresholdMs(): Long = mainThreadStallThresholdMs

    fun memorySampleIntervalMs(): Long = memorySampleIntervalMs

    fun fpsMonitorEnabled(): Boolean = fpsMonitorEnabled

    fun fpsWindowMs(): Long = fpsWindowMs

    fun jankFrameThresholdMs(): Long = jankFrameThresholdMs

    fun maxQueueSize(): Int = maxQueueSize

    fun maxLogBytes(): Long = maxLogBytes

    fun flushIntervalMs(): Long = flushIntervalMs

    fun logDirectory(): File? = logDirectory

    class Builder {
        internal var enabled = true
        internal var autoStartCollectors = true
        internal var mainThreadStallThresholdMs = 700L
        internal var memorySampleIntervalMs = 10_000L
        internal var fpsMonitorEnabled = true
        internal var fpsWindowMs = 1_000L
        internal var jankFrameThresholdMs = 32L
        internal var maxQueueSize = 2048
        internal var maxLogBytes = 5L * 1024L * 1024L
        internal var flushIntervalMs = 5_000L
        internal var logDirectory: File? = null

        fun enabled(value: Boolean) = apply { enabled = value }

        fun autoStartCollectors(value: Boolean) = apply { autoStartCollectors = value }

        fun mainThreadStallThresholdMs(value: Long) = apply { mainThreadStallThresholdMs = value }

        fun memorySampleIntervalMs(value: Long) = apply { memorySampleIntervalMs = value }

        fun fpsMonitorEnabled(value: Boolean) = apply { fpsMonitorEnabled = value }

        fun fpsWindowMs(value: Long) = apply { fpsWindowMs = value }

        fun jankFrameThresholdMs(value: Long) = apply { jankFrameThresholdMs = value }

        fun maxQueueSize(value: Int) = apply { maxQueueSize = value }

        fun maxLogBytes(value: Long) = apply { maxLogBytes = value }

        fun flushIntervalMs(value: Long) = apply { flushIntervalMs = value }

        fun logDirectory(value: File?) = apply { logDirectory = value }

        fun build(): JankHunterConfig = JankHunterConfig(this)
    }

    companion object {
        @JvmStatic
        fun builder(): Builder = Builder()
    }
}
