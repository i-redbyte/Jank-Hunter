package io.jankhunter.runtime.internal.io

import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Test

class LogQualityCountersTest {
    @Test
    fun snapshotsNeverTearMultiCounterUpdates() {
        val counters = LogQualityCounters()
        val workers = List(WORKER_COUNT) {
            thread(start = true) {
                repeat(UPDATES_PER_WORKER) {
                    counters.addRejected(
                        JhlogV9.TYPE_RUNTIME_CALL,
                        QualityCounterId.REASON_QUEUE_FULL,
                    )
                }
            }
        }

        val perEventId = QualityCounterId.eventReason(
            JhlogV9.TYPE_RUNTIME_CALL,
            QualityCounterId.REASON_QUEUE_FULL,
        )
        while (workers.any { it.isAlive }) {
            val snapshot = counters.snapshot().associate { it.counterId to it.value }
            assertEquals(
                snapshot[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L,
                snapshot[perEventId] ?: 0L,
            )
        }
        workers.forEach { it.join() }

        val finalSnapshot = counters.snapshot().associate { it.counterId to it.value }
        val expected = WORKER_COUNT.toLong() * UPDATES_PER_WORKER
        assertEquals(expected, finalSnapshot[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
        assertEquals(expected, finalSnapshot[perEventId] ?: 0L)
    }

    @Test
    fun freezeCreatesImmutableTerminalBoundary() {
        val counters = LogQualityCounters()
        counters.addAccepted()

        counters.freeze()
        counters.addAccepted()

        val snapshot = counters.snapshot().associate { it.counterId to it.value }
        assertEquals(1L, snapshot[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
    }

    private companion object {
        const val WORKER_COUNT = 4
        const val UPDATES_PER_WORKER = 5_000
    }
}
