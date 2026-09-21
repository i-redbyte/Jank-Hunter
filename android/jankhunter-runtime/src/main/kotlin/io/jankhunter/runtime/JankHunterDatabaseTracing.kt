package io.jankhunter.runtime

/** Dependency-free manual adapter API for custom databases and ORMs. */
object JankHunterDatabaseTracing {
    @JvmStatic
    @JvmOverloads
    fun beginCall(
        sourceAlias: String,
        sqlTemplate: String?,
        operation: JankHunterDatabaseOperation,
        boundary: JankHunterDatabaseBoundary = JankHunterDatabaseBoundary.MANUAL,
    ): JankHunterDatabaseCallToken? = JankHunter.databaseTracing().beginCall(
        sourceAlias,
        sqlTemplate,
        operation,
        boundary,
    )

    @JvmStatic
    @JvmOverloads
    fun endCall(
        token: JankHunterDatabaseCallToken?,
        resultKind: JankHunterDatabaseResultKind? = null,
        resultCount: Long = -1L,
        failure: Throwable? = null,
    ) = JankHunter.databaseTracing().endCall(token, resultKind, resultCount, failure)

    @JvmStatic
    fun beginPhase(token: JankHunterDatabaseCallToken?): Long = JankHunter.databaseTracing().beginPhase(token)

    @JvmStatic
    fun endPhase(token: JankHunterDatabaseCallToken?, phase: JankHunterDatabasePhase, startedNanos: Long) {
        JankHunter.databaseTracing().endPhase(token, phase, startedNanos)
    }

    @JvmSynthetic
    inline fun <T> traceCall(
        sourceAlias: String,
        sqlTemplate: String?,
        operation: JankHunterDatabaseOperation,
        boundary: JankHunterDatabaseBoundary = JankHunterDatabaseBoundary.MANUAL,
        block: () -> T,
    ): T {
        val token = beginCall(sourceAlias, sqlTemplate, operation, boundary) ?: return block()
        try {
            return block().also { endCall(token) }
        } catch (throwable: Throwable) {
            endCall(token, failure = throwable)
            throw throwable
        }
    }

    @JvmStatic
    @JvmOverloads
    fun beginTransaction(
        sourceAlias: String,
        mode: JankHunterDatabaseTransactionMode = JankHunterDatabaseTransactionMode.DEFERRED,
    ): JankHunterDatabaseTransactionToken? = JankHunter.databaseTracing().beginTransaction(sourceAlias, mode)

    @JvmStatic
    @JvmOverloads
    fun endTransaction(
        token: JankHunterDatabaseTransactionToken?,
        outcome: JankHunterDatabaseTransactionOutcome,
        failure: Throwable? = null,
    ) = JankHunter.databaseTracing().endTransaction(token, outcome, failure)

    @JvmSynthetic
    inline fun <T> traceTransaction(
        sourceAlias: String,
        mode: JankHunterDatabaseTransactionMode = JankHunterDatabaseTransactionMode.DEFERRED,
        block: () -> T,
    ): T {
        val token = beginTransaction(sourceAlias, mode) ?: return block()
        try {
            return block().also {
                endTransaction(token, JankHunterDatabaseTransactionOutcome.SUCCESS)
            }
        } catch (throwable: Throwable) {
            endTransaction(token, JankHunterDatabaseTransactionOutcome.FAILURE, throwable)
            throw throwable
        }
    }
}
