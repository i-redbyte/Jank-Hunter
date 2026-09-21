package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterLogGrowthSummary
import io.jankhunter.runtime.RuntimeLongSource
import java.io.File

internal class LogGrowthManager(
    directory: File,
    processScope: String? = null,
    private val nowMs: RuntimeLongSource = RuntimeLongSource(System::currentTimeMillis),
) {
    private val store = LogGrowthHistoryStore(directory, processScope)
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
            limitReachedCount = stats.limitReachedCount.coerceAtLeast(0L),
            segmentRotationCount = stats.segmentRotationCount.coerceAtLeast(0L),
            archiveEvictedBytes = stats.archiveEvictedBytes.coerceAtLeast(0L),
            firstLimitReachedAtMs = 0L,
            lastLimitReachedAtMs = 0L,
        )
        state = store.writeActive(state, active)
        val persisted = requireNotNull(state.active)
        return StartedLogGrowth(history, LogGrowthWire.live(persisted, completed = false))
    }

    @Synchronized
    fun checkpoint(stats: LogContainerStats): ByteArray? {
        val current = state.active ?: return null
        state = store.writeActive(state, updateActive(current, stats, nowMs.getAsLong()))
        return state.active?.let { LogGrowthWire.live(it, completed = false) }
    }

    @Synchronized
    fun complete(stats: LogContainerStats): ByteArray? {
        val current = state.active ?: return null
        state = store.writeActive(state, updateActive(current, stats, nowMs.getAsLong()))
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
        val capturedAt = nowMs.getAsLong().coerceAtLeast(0L)
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
        val limitReached = maxOf(current.limitReachedCount, stats.limitReachedCount.coerceAtLeast(0L))
        val newlyReachedLimit = limitReached > current.limitReachedCount
        val time = maxOf(current.startedAtMs, updatedAtMs)
        return current.copy(
            updatedAtMs = maxOf(current.updatedAtMs, time),
            maximumRetainedBytes = maxOf(current.maximumRetainedBytes, stats.retainedBytes.coerceAtLeast(0L)),
            generatedBytes = maxOf(current.generatedBytes, stats.generatedBytes.coerceAtLeast(0L)),
            limitReachedCount = limitReached,
            segmentRotationCount = maxOf(
                current.segmentRotationCount,
                stats.segmentRotationCount.coerceAtLeast(0L),
            ),
            archiveEvictedBytes = maxOf(
                current.archiveEvictedBytes,
                stats.archiveEvictedBytes.coerceAtLeast(0L),
            ),
            firstLimitReachedAtMs = when {
                current.firstLimitReachedAtMs > 0L -> current.firstLimitReachedAtMs
                newlyReachedLimit -> time
                else -> 0L
            },
            lastLimitReachedAtMs = if (newlyReachedLimit) time else current.lastLimitReachedAtMs,
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
    val baseStats: LogContainerStats = LogContainerStats.EMPTY,
)
