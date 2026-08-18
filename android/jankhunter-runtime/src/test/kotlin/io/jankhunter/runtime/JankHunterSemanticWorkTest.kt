package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterSemanticWorkTest {
    @Test
    fun workerResultNamesAreClassifiedWithoutWorkManagerDependency() {
        assertEquals(JankHunterWorkerOutcome.SUCCESS.code, JankHunter.classifyWorkerOutcome(Success()))
        assertEquals(JankHunterWorkerOutcome.FAILURE.code, JankHunter.classifyWorkerOutcome(Failure()))
        assertEquals(JankHunterWorkerOutcome.RETRY.code, JankHunter.classifyWorkerOutcome(Retry()))
        assertEquals(JankHunterWorkerOutcome.SUCCESS.code, JankHunter.classifyWorkerOutcome(PlainResult()))
        assertEquals(JankHunterWorkerOutcome.UNKNOWN.code, JankHunter.classifyWorkerOutcome(null))
    }

    private class Success
    private class Failure
    private class Retry
    private class PlainResult
}
