package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogGrowthSummary
import java.io.File

internal class LogGrowthManager(
    directory: File,
    private val nowMs: () -> Long = System::currentTimeMillis,
) {
    private val store = LogGrowthHistoryStore(directory)
    private var state = store.load()

    @Synchronized
    fun beginSession(
        sessionId: ByteArray,
        localDate: String,
        startedAtMs: Long,
        configuredLimitBytes: Long,
        stats: LogContainerStats,
    ): StartedLogGrowth {
        recoverInterruptedSession()
        val history = LogGrowthWire.history(state, startedAtMs)
        val (idHigh, idLow) = sessionIdParts(sessionId)
        val started = startedAtMs.coerceAtLeast(0L)
        val active = ActiveLogGrowthFact(
            generation = 0L,
            idHigh = idHigh,
            idLow = idLow,
            dayKey = dayKey(localDate),
            startedAtMs = started,
            updatedAtMs = started,
            configuredLimitBytes = configuredLimitBytes.coerceAtLeast(0L),
            maximumRetainedBytes = stats.retainedBytes.coerceAtLeast(0L),
            generatedBytes = stats.generatedBytes.coerceAtLeast(0L),
            overflowCount = stats.overflowCount.coerceAtLeast(0L),
            evictedChunkCount = stats.evictedChunkCount.coerceAtLeast(0L),
            evictedBytes = stats.evictedBytes.coerceAtLeast(0L),
            firstOverflowAtMs = 0L,
            lastOverflowAtMs = 0L,
        )
        state = store.writeActive(state, active)
        val persisted = requireNotNull(state.active)
        return StartedLogGrowth(history, LogGrowthWire.live(persisted, completed = false))
    }

    @Synchronized
    fun onChunkCommitted(stats: LogContainerStats): ByteArray? {
        val current = state.active ?: return null
        if (stats.overflowCount <= current.overflowCount) return null
        val updated = updateActive(current, stats, nowMs())
        state = store.writeActive(state, updated)
        return state.active?.let { LogGrowthWire.live(it, completed = false) }
    }

    @Synchronized
    fun checkpoint(stats: LogContainerStats): ByteArray? {
        val current = state.active ?: return null
        state = store.writeActive(state, updateActive(current, stats, nowMs()))
        return state.active?.let { LogGrowthWire.live(it, completed = false) }
    }

    @Synchronized
    fun complete(stats: LogContainerStats): ByteArray? {
        val current = state.active ?: return null
        state = store.writeActive(state, updateActive(current, stats, nowMs()))
        val finalActive = state.active ?: return null
        val completed = finalActive.toSessionFact(
            sequence = state.nextSessionSequence,
            commitGeneration = state.generation + 1L,
            recovered = false,
        )
        state = store.complete(state, completed)
        return LogGrowthWire.live(finalActive, completed = true)
    }

    @Synchronized
    fun summary(currentStats: LogContainerStats? = null): JankHunterLogGrowthSummary {
        val capturedAt = nowMs().coerceAtLeast(0L)
        val current = state.active?.let { active ->
            val currentFact = if (currentStats == null) active else updateActive(active, currentStats, capturedAt)
            currentFact.copy(updatedAtMs = maxOf(currentFact.updatedAtMs, capturedAt))
                .toSessionFact(-1L, state.generation, recovered = false)
                .toPublic(completed = false)
        }
        return JankHunterLogGrowthSummary(
            enabled = true,
            capturedAtMs = capturedAt,
            currentSession = current,
            recentSessions = state.sessions.map(LogGrowthSessionFact::toPublic),
            days = state.days.map(LogGrowthDayFact::toPublic),
        )
    }

    private fun recoverInterruptedSession() {
        val interrupted = state.active ?: return
        if (state.sessions.any { it.idHigh == interrupted.idHigh && it.idLow == interrupted.idLow }) {
            state = store.writeActive(state, null)
            return
        }
        val recovered = interrupted.toSessionFact(
            sequence = state.nextSessionSequence,
            commitGeneration = state.generation + 1L,
            recovered = true,
        )
        state = store.complete(state, recovered)
    }

    private fun updateActive(
        current: ActiveLogGrowthFact,
        stats: LogContainerStats,
        updatedAtMs: Long,
    ): ActiveLogGrowthFact {
        val overflows = maxOf(current.overflowCount, stats.overflowCount.coerceAtLeast(0L))
        val newOverflow = overflows > current.overflowCount
        val time = maxOf(current.startedAtMs, updatedAtMs)
        return current.copy(
            updatedAtMs = maxOf(current.updatedAtMs, time),
            maximumRetainedBytes = maxOf(current.maximumRetainedBytes, stats.retainedBytes.coerceAtLeast(0L)),
            generatedBytes = maxOf(current.generatedBytes, stats.generatedBytes.coerceAtLeast(0L)),
            overflowCount = overflows,
            evictedChunkCount = maxOf(current.evictedChunkCount, stats.evictedChunkCount.coerceAtLeast(0L)),
            evictedBytes = maxOf(current.evictedBytes, stats.evictedBytes.coerceAtLeast(0L)),
            firstOverflowAtMs = when {
                current.firstOverflowAtMs > 0L -> current.firstOverflowAtMs
                newOverflow -> time
                else -> 0L
            },
            lastOverflowAtMs = if (newOverflow) time else current.lastOverflowAtMs,
        )
    }
}

internal data class StartedLogGrowth(
    val history: ByteArray,
    val live: ByteArray,
)

internal data class LogGrowthSessionBinding(
    val manager: LogGrowthManager,
    val localDate: String,
    val configuredLimitBytes: Long,
)
