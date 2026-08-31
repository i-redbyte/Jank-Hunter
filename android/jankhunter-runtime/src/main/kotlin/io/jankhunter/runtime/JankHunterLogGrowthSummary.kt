package io.jankhunter.runtime

import java.util.Collections

class JankHunterLogGrowthSummary internal constructor(
    val enabled: Boolean,
    val capturedAtMs: Long,
    val currentSession: JankHunterLogGrowthSessionSummary?,
    recentSessions: List<JankHunterLogGrowthSessionSummary>,
    days: List<JankHunterLogGrowthDaySummary>,
) {
    val recentSessions: List<JankHunterLogGrowthSessionSummary> =
        Collections.unmodifiableList(ArrayList(recentSessions))
    val days: List<JankHunterLogGrowthDaySummary> =
        Collections.unmodifiableList(ArrayList(days))

}

class JankHunterLogGrowthSessionSummary internal constructor(
    val sessionId: String,
    val localDate: String,
    val startedAtMs: Long,
    val endedAtMs: Long,
    val durationMs: Long,
    val configuredLimitBytes: Long,
    val maximumRetainedBytes: Long,
    val generatedBytes: Long,
    val averageGrowthBytesPerMinute: Long,
    val reachedLimit: Boolean,
    val limitReachedCount: Long,
    val segmentRotationCount: Long,
    val archiveEvictedBytes: Long,
    val firstLimitReachedAtMs: Long,
    val lastLimitReachedAtMs: Long,
    val completed: Boolean,
    val recoveredAfterInterruption: Boolean,
)

class JankHunterLogGrowthDaySummary internal constructor(
    val localDate: String,
    val sessionCount: Long,
    val totalDurationMs: Long,
    val generatedBytes: Long,
    val maximumRetainedBytes: Long,
    val maximumFillPermille: Long,
    val sessionsReachingLimit: Long,
    val limitReachedCount: Long,
    val segmentRotationCount: Long,
    val archiveEvictedBytes: Long,
)
