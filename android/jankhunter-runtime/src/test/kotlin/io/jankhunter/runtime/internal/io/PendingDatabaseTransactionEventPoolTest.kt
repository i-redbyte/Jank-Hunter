package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class PendingDatabaseTransactionEventPoolTest {
    @Test
    fun recycledEntryIsReusedAfterPayloadReferencesAreCleared() {
        val pool = PendingDatabaseTransactionEventPool(1)
        val event = pool.acquire(
            producerContext = LogEventContext("screen", "owner", 7L),
            sourceId = 1L,
            sourceName = "Database.transaction",
            transactionId = 2L,
            parentId = 0L,
            stage = Jhlog.DATABASE_TRANSACTION_BEGIN,
            mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
            outcome = Jhlog.DATABASE_TRANSACTION_OUTCOME_UNKNOWN,
            failureKind = Jhlog.DATABASE_FAILURE_NONE,
            durationUs = 0L,
            statementCount = 0L,
            readCount = 0L,
            writeCount = 0L,
            mainThread = false,
        )

        event.recycle()

        assertNull(field(PendingLogEvent::class.java, "producerContext").get(event))
        assertNull(field(PendingDatabaseTransactionEvent::class.java, "sourceName").get(event))
        assertSame(
            event,
            pool.acquire(
                producerContext = null,
                sourceId = 3L,
                sourceName = null,
                transactionId = 4L,
                parentId = 0L,
                stage = Jhlog.DATABASE_TRANSACTION_TERMINAL,
                mode = Jhlog.DATABASE_TRANSACTION_DEFERRED,
                outcome = Jhlog.DATABASE_TRANSACTION_SUCCESS,
                failureKind = Jhlog.DATABASE_FAILURE_NONE,
                durationUs = 5L,
                statementCount = 1L,
                readCount = 1L,
                writeCount = 0L,
                mainThread = false,
            ),
        )
    }

    private fun field(type: Class<*>, name: String) = type.getDeclaredField(name).apply { isAccessible = true }
}
