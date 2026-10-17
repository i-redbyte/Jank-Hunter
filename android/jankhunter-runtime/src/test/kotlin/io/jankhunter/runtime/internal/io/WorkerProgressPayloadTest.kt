package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterWorkDataField
import org.junit.Assert.assertEquals
import org.junit.Test

class WorkerProgressPayloadTest {
    @Test fun requestAndSummaryUseDedicatedBoundedRecords() {
        val request = WorkerProgressObservation(1, 73, 2, 1, 987654321, 0,
            listOf(JankHunterWorkDataField("done", "int", 4)), 2, 1)
        val summary = WorkerProgressObservation(2, 73, 2, 1, 987654322, 5, emptyList(), 0, 0)
        assertEquals(Jhlog.TYPE_WORKER_PROGRESS, PendingWorkerProgress(null, request).recordType)
        encodeWorkerProgress(BinaryPayload(), request)
        encodeWorkerProgress(BinaryPayload(), summary)
    }

    @Test(expected = IllegalArgumentException::class)
    fun tooManyFieldsAreRejected() {
        encodeWorkerProgress(BinaryPayload(), WorkerProgressObservation(1, 1, 0, 0, 1, 0,
            List(9) { JankHunterWorkDataField("k$it", "int", 4) }, 0, 0))
    }
}
