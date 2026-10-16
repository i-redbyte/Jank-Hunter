package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class WorkerFailurePayloadTest {
    @Test fun exceptionUsesDedicatedSemanticRecord() {
        val observation = WorkerFailureObservation(73, 2, 1, 987654321, "app.SyncException",
            "app.SyncWorker.doWork:42", false)
        assertEquals(Jhlog.TYPE_WORKER_FAILURE, PendingWorkerFailure(null, observation).recordType)
        encodeWorkerFailure(BinaryPayload(), observation)
    }

    @Test(expected = IllegalArgumentException::class)
    fun oversizedStackIsRejected() {
        encodeWorkerFailure(BinaryPayload(), WorkerFailureObservation(1, 0, 0, 1, "Failure",
            "x".repeat(1025), true))
    }

    @Test(expected = IllegalStateException::class)
    fun type36CannotEnterFiveBitMicroPage() {
        ColumnarMicroPage().append(Jhlog.TYPE_WORKER_FAILURE, 0, BinaryPayload(), null, null, 1)
    }
}
