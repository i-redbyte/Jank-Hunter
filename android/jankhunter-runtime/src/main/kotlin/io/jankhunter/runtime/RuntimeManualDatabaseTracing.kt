package io.jankhunter.runtime

import android.os.SystemClock

/** Manual database instrumentation that never retains database, statement, cursor, or bind values. */
internal class RuntimeManualDatabaseTracing(
    private val telemetry: RuntimeDatabaseTelemetry,
) {
    fun beginCall(
        sourceAlias: String,
        sqlTemplate: String?,
        operation: JankHunterDatabaseOperation,
        boundary: JankHunterDatabaseBoundary,
    ): JankHunterDatabaseCallToken? {
        if (!telemetry.isEnabled()) return null
        val sourceName = normalizeSourceAlias(sourceAlias) ?: return null
        val query = RuntimeSqlNormalizer.normalize(sqlTemplate)
        return JankHunterDatabaseCallToken(
            sourceId = sourceId(sourceName),
            sourceName = sourceName,
            query = query,
            fingerprint = RuntimeSqlNormalizer.fingerprint(query),
            operation = operation.wireValue,
            boundary = boundary.wireValue,
            startedNanos = SystemClock.elapsedRealtimeNanos().coerceAtLeast(1L),
        )
    }

    fun endCall(
        token: JankHunterDatabaseCallToken?,
        resultKind: JankHunterDatabaseResultKind?,
        resultCount: Long,
        failure: Throwable?,
    ) {
        if (token == null || !token.completeOnce()) return
        telemetry.recordManual(token, resultKind, resultCount, failure)
    }

    fun beginPhase(token: JankHunterDatabaseCallToken?): Long {
        if (token == null || !telemetry.isEnabled()) return 0L
        return SystemClock.elapsedRealtimeNanos().coerceAtLeast(1L)
    }

    fun endPhase(token: JankHunterDatabaseCallToken?, phase: JankHunterDatabasePhase, startedNanos: Long) {
        if (token == null || startedNanos <= 0L || startedNanos < token.startedNanos || !telemetry.isEnabled()) {
            return
        }
        token.recordPhase(
            phase,
            (SystemClock.elapsedRealtimeNanos() - startedNanos).coerceAtLeast(0L),
        )
    }

    fun beginTransaction(
        sourceAlias: String,
        mode: JankHunterDatabaseTransactionMode,
    ): JankHunterDatabaseTransactionToken? {
        if (!telemetry.isEnabled()) return null
        val sourceName = normalizeSourceAlias(sourceAlias) ?: return null
        return telemetry.beginManualTransaction(sourceId(sourceName), sourceName, mode.wireValue)
    }

    fun endTransaction(
        token: JankHunterDatabaseTransactionToken?,
        outcome: JankHunterDatabaseTransactionOutcome,
        failure: Throwable?,
    ) {
        if (token == null || !token.completeOnce()) return
        telemetry.endManualTransaction(token, outcome, failure)
    }

    private fun normalizeSourceAlias(sourceAlias: String): String? {
        val value = sourceAlias.trim()
        if (value.isEmpty()) return null
        return if (value.length <= MAX_SOURCE_ALIAS_CHARS) value else value.substring(0, MAX_SOURCE_ALIAS_CHARS)
    }

    private fun sourceId(sourceName: String): Long {
        val value = JankHunterSemanticWork.stableId("jankhunter.database.manual.v1\u0000$sourceName")
        return if (value == 0L) 1L else value
    }

    private companion object {
        const val MAX_SOURCE_ALIAS_CHARS = 320
    }
}
