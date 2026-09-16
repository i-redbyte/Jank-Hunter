package io.jankhunter.runtime

import java.nio.charset.StandardCharsets
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterSemanticWorkTest {
    @Test
    fun streamingStableIdMatchesExistingUtf8Encoding() {
        val prefix = "jankhunter.semantic.target.v1\u0000"
        val values = listOf(
            "ascii",
            "Привет",
            "emoji-\uD83D\uDE80",
            "unpaired-high-\uD800",
            "unpaired-low-\uDC00",
        )

        for (value in values) {
            assertEquals(referenceStableId(prefix + value), JankHunterSemanticWork.stableId(prefix, value))
        }
    }

    @Test
    fun finiteCallerMetadataIsPrecomputed() {
        val first = JankHunterSemanticWork.caller(
            JankHunterSemanticWork.WORKER,
            mainThread = false,
            JankHunterWorkerOutcome.RETRY,
        )
        val second = JankHunterSemanticWork.caller(
            JankHunterSemanticWork.WORKER,
            mainThread = false,
            JankHunterWorkerOutcome.RETRY,
        )

        assertSame(first, second)
    }

    @Test
    fun precomputedCallerIdsUseInitializedStableHashConstants() {
        val ids = hashSetOf<Long>()
        for (kind in JankHunterSemanticWork.COMPOSE_COMPOSITION..JankHunterSemanticWork.WORKER) {
            for (mainThread in listOf(false, true)) {
                val outcomes = if (kind == JankHunterSemanticWork.WORKER) {
                    JankHunterWorkerOutcome.entries
                } else {
                    listOf(null)
                }
                for (outcome in outcomes) {
                    val caller = checkNotNull(JankHunterSemanticWork.caller(kind, mainThread, outcome))
                    assertEquals(JankHunterSemanticWork.stableId(caller.name), caller.id)
                    assertNotEquals(0L, caller.id)
                    assertTrue("duplicate caller ID for ${caller.name}", ids.add(caller.id))
                }
            }
        }
    }

    @Test
    fun workerResultNamesAreClassifiedWithoutWorkManagerDependency() {
        assertEquals(JankHunterWorkerOutcome.SUCCESS.code, JankHunterHooks.classifyWorkerOutcome(Success()))
        assertEquals(JankHunterWorkerOutcome.FAILURE.code, JankHunterHooks.classifyWorkerOutcome(Failure()))
        assertEquals(JankHunterWorkerOutcome.RETRY.code, JankHunterHooks.classifyWorkerOutcome(Retry()))
        assertEquals(JankHunterWorkerOutcome.UNKNOWN.code, JankHunterHooks.classifyWorkerOutcome(PlainResult()))
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

    private fun referenceStableId(value: String): Long {
        var hash = 0xcbf29ce484222325UL
        for (byte in value.toByteArray(StandardCharsets.UTF_8)) {
            hash = hash xor byte.toUByte().toULong()
            hash *= 0x100000001b3UL
        }
        return hash.toLong()
    }
}
