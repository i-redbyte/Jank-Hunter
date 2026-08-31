package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterSemanticWorkTest {
    @Test
    fun workerResultNamesAreClassifiedWithoutWorkManagerDependency() {
        assertEquals(JankHunterWorkerOutcome.SUCCESS.code, JankHunterHooks.classifyWorkerOutcome(Success()))
        assertEquals(JankHunterWorkerOutcome.FAILURE.code, JankHunterHooks.classifyWorkerOutcome(Failure()))
        assertEquals(JankHunterWorkerOutcome.RETRY.code, JankHunterHooks.classifyWorkerOutcome(Retry()))
        assertEquals(JankHunterWorkerOutcome.SUCCESS.code, JankHunterHooks.classifyWorkerOutcome(PlainResult()))
        assertEquals(JankHunterWorkerOutcome.UNKNOWN.code, JankHunterHooks.classifyWorkerOutcome(null))
        assertEquals(JankHunterWorkerOutcome.SUCCESS, JankHunterWorkerRuntime.classify(Success()))
        assertEquals(JankHunterWorkerOutcome.FAILURE, JankHunterWorkerRuntime.classify(Failure()))
        assertEquals(JankHunterWorkerOutcome.RETRY, JankHunterWorkerRuntime.classify(Retry()))
        assertEquals(JankHunterWorkerOutcome.UNKNOWN, JankHunterWorkerRuntime.classify(null))
    }

    @Test
    fun workerInstanceIdsAreProcessPrivateStableAndNonZero() {
        val first = JankHunterWorkerRuntime.instanceId(0x1234L, 0x5678L)
        assertEquals(first, JankHunterWorkerRuntime.instanceId(0x1234L, 0x5678L))
        org.junit.Assert.assertNotEquals(first, JankHunterWorkerRuntime.instanceId(0x1234L, 0x5679L))
        org.junit.Assert.assertNotEquals(0L, first)
    }

    private class Success
    private class Failure
    private class Retry
    private class PlainResult
}
