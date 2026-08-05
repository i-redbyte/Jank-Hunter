package io.jankhunter.runtime.internal.system

import java.util.concurrent.atomic.AtomicBoolean

internal class ForegroundSamplingSchedule(
    private val intervalMs: Long,
    private val foreground: () -> Boolean,
    private val task: () -> Unit,
    private val minForegroundIntervalMs: Long = 1_000L,
    private val backgroundIntervalMultiplier: Long = 12L,
    private val minBackgroundIntervalMs: Long = 2 * 60_000L,
) {
    private val running = AtomicBoolean(false)
    private val lock = Any()
    private var scheduler: RuntimeMaintenanceScheduler? = null
    private var maintenance: MaintenanceHandle? = null

    fun start(scheduler: RuntimeMaintenanceScheduler) {
        if (!running.compareAndSet(false, true)) return
        synchronized(lock) {
            this.scheduler = scheduler
            schedule(0)
        }
    }

    fun stop() {
        if (!running.compareAndSet(true, false)) return
        synchronized(lock) {
            maintenance?.cancel()
            maintenance = null
            scheduler = null
        }
    }

    fun onForegroundChanged() {
        if (!running.get()) return
        synchronized(lock) {
            maintenance?.cancel()
            schedule(currentIntervalMs())
        }
    }

    private fun schedule(initialDelayMs: Long) {
        if (!running.get()) return
        maintenance = scheduler?.schedule(
            initialDelayMs = initialDelayMs,
            delayMs = ::currentIntervalMs,
            task = task,
        )
    }

    private fun currentIntervalMs(): Long {
        val foregroundInterval = intervalMs.coerceAtLeast(minForegroundIntervalMs.coerceAtLeast(1L))
        if (foreground()) return foregroundInterval
        val multiplier = backgroundIntervalMultiplier.coerceAtLeast(1L)
        val backgroundInterval = if (foregroundInterval > Long.MAX_VALUE / multiplier) {
            Long.MAX_VALUE
        } else {
            foregroundInterval * multiplier
        }
        return maxOf(minBackgroundIntervalMs.coerceAtLeast(1L), backgroundInterval)
    }
}
