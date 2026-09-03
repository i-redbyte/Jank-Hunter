package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class PendingDatabaseEventPoolTest {
    @Test
    fun recycledEntryIsReusedAfterPayloadReferencesAreCleared() {
        val pool = PendingDatabaseEventPool(1)
        val event = pool.acquire(
            producerContext = LogEventContext("screen", "owner", 7L),
            sourceId = 1L,
            sourceName = "Database.query",
            query = "SELECT value FROM table",
            framework = Jhlog.DATABASE_FRAMEWORK_SQLITE,
            operation = Jhlog.DATABASE_OPERATION_QUERY,
            outcome = Jhlog.DATABASE_OUTCOME_SUCCESS,
            durationUs = 1_000L,
            mainThread = false,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE,
            statementFingerprint = 2L,
            resultKnown = false,
            resultKind = Jhlog.DATABASE_RESULT_UNKNOWN,
            resultCountBucket = Jhlog.DATABASE_COUNT_UNKNOWN,
            transactionId = 0L,
            statementToken = 0L,
            phaseMask = 0L,
            poolWaitUs = 0L,
            lockWaitUs = 0L,
            executeUs = 0L,
            materializeUs = 0L,
        )

        event.recycle()

        assertNull(field(PendingLogEvent::class.java, "producerContext").get(event))
        assertNull(field(PendingDatabaseEvent::class.java, "sourceName").get(event))
        assertNull(field(PendingDatabaseEvent::class.java, "query").get(event))
        assertSame(
            event,
            pool.acquire(
                producerContext = null,
                sourceId = 3L,
                sourceName = null,
                query = null,
                framework = Jhlog.DATABASE_FRAMEWORK_ROOM,
                operation = Jhlog.DATABASE_OPERATION_UPDATE,
                outcome = Jhlog.DATABASE_OUTCOME_SUCCESS,
                durationUs = 2_000L,
                mainThread = false,
                failureKind = Jhlog.DATABASE_FAILURE_NONE,
                boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE,
                statementFingerprint = 4L,
                resultKnown = false,
                resultKind = Jhlog.DATABASE_RESULT_UNKNOWN,
                resultCountBucket = Jhlog.DATABASE_COUNT_UNKNOWN,
                transactionId = 0L,
                statementToken = 0L,
                phaseMask = 0L,
                poolWaitUs = 0L,
                lockWaitUs = 0L,
                executeUs = 0L,
                materializeUs = 0L,
            ),
        )
    }

    private fun field(type: Class<*>, name: String) = type.getDeclaredField(name).apply { isAccessible = true }
}
