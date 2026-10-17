package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class WorkChainRootPayloadTest {
    @Test fun rootObservationUsesDedicatedEventType() {
        val pending = PendingWorkChainRoot(null, WorkSubmissionObservation(
            1, 2, 3, false, "sync", WorkSubmissionPolicy.KEEP, 0,
            callerSource = "app#beginUniqueWork",
        ))
        assertEquals(Jhlog.TYPE_WORK_CHAIN_ROOT, pending.recordType)
    }

    @Test(expected = IllegalStateException::class)
    fun type35CannotEnterFiveBitMicroPage() {
        ColumnarMicroPage().append(Jhlog.TYPE_WORK_CHAIN_ROOT, 0, BinaryPayload(), null, null, 1)
    }
}
