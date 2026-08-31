package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeBooleanSource
import java.util.concurrent.atomic.AtomicBoolean

/**
 * Changes collector cadence from a single user-relevance signal. The signal deliberately combines
 * visible UI and foreground-process execution without claiming that a foreground service owns UI.
 */
internal class UserRelevantSamplingSchedule(
    private val intervalMs: Long,
    private val userRelevant: RuntimeBooleanSource,
    private val task: () -> Unit,
    private val minActiveIntervalMs: Long = 1_000L,
    private val inactiveIntervalMultiplier: Long = 12L,
    private val minInactiveIntervalMs: Long = 2 * 60_000L,
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

    fun onUserRelevanceChanged() {
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
        val activeInterval = intervalMs.coerceAtLeast(minActiveIntervalMs.coerceAtLeast(1L))
        if (userRelevant.getAsBoolean()) return activeInterval
        val multiplier = inactiveIntervalMultiplier.coerceAtLeast(1L)
        val inactiveInterval = if (activeInterval > Long.MAX_VALUE / multiplier) {
            Long.MAX_VALUE
        } else {
            activeInterval * multiplier
        }
        return maxOf(minInactiveIntervalMs.coerceAtLeast(1L), inactiveInterval)
    }
}
