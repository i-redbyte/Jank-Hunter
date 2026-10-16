package io.jankhunter.workmanager

import androidx.work.WorkInfo
import io.jankhunter.runtime.JankHunterWorkerOutcome
import org.junit.Assert.assertEquals
import org.junit.Test

class WorkerStopOutcomeTest {
    @Test fun constraintStopDoesNotClaimApplicationCancellation() {
        assertEquals(JankHunterWorkerOutcome.STOPPED,
            workerTerminalOutcome(true, JankHunterWorkerOutcome.SUCCESS, true, WorkInfo.STOP_REASON_CONSTRAINT_CONNECTIVITY))
        assertEquals(JankHunterWorkerOutcome.STOPPED,
            workerTerminalOutcome(true, JankHunterWorkerOutcome.CANCELLED, false, 0))
    }
    @Test fun cancellationRequiresEvidenceAndNormalResultsRemainUnchanged() {
        assertEquals(JankHunterWorkerOutcome.CANCELLED,
            workerTerminalOutcome(true, JankHunterWorkerOutcome.SUCCESS, true, WorkInfo.STOP_REASON_CANCELLED_BY_APP))
        assertEquals(JankHunterWorkerOutcome.RETRY,
            workerTerminalOutcome(false, JankHunterWorkerOutcome.RETRY, true, 0))
    }
}
