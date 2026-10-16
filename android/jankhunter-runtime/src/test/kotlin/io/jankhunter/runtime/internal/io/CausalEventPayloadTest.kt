package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class CausalEventPayloadTest {
    @Test
    fun allOptionalFieldsUseTheirFrozenPresenceBitsAndTailOrder() {
        var covered = 0L
        fun verify(event: CausalEventObservation, bits: Long, tail: BinaryPayload) {
            assertTail(event, bits, tail)
            covered = covered or bits
        }

        verify(CausalEventObservation(CausalKind.COROUTINE, CausalPhase.Coroutine.SUSPENDED, 1L,
            observedNanos = 2L, targetJavaId = 11L, targetTid = 22L, cpuNanos = 0L,
            durationNanos = 0L, coroutineToken = 77L, coroutineGeneration = 3L),
            bits(0, 1, 4, 5, 17), BinaryPayload().uvarint(11).uvarint(22).uvarint(0)
                .uvarint(0).uvarint(77).uvarint(3))
        verify(CausalEventObservation(CausalKind.OPERATION, 1, 2L, observedNanos = 3L,
            sourceOrigin = 1, sourceSiteId = 9L, sourceMethod = "a.A#read()V", sourceLine = 13,
            durationNanos = 0L, readBytes = 0L, writtenBytes = 5L, operationKind = 1, leaseId = 55L),
            bits(2, 5, 6, 7, 13, 16), BinaryPayload().uvarint(9).boundedString("a.A#read()V", 512)
                .uvarint(13).uvarint(0).uvarint(0).uvarint(5).uvarint(1).uvarint(55))
        verify(CausalEventObservation(CausalKind.EXECUTOR_TASK, CausalPhase.ExecutorTask.SUBMITTED, 3L,
            observedNanos = 4L, scheduledForNanos = 0L, queueEnteredNanos = 0L, occurrence = 2L),
            bits(8, 9, 10), BinaryPayload().uvarint(0).uvarint(0).uvarint(2))
        val unsignedWorkerId = 0xfedcba9876543210UL.toLong()
        verify(CausalEventObservation(CausalKind.WORKER_ATTEMPT, CausalPhase.WorkerAttempt.BOUND, 4L,
            observedNanos = 5L, appCallerSiteId = 22L, appCallerMethod = "a.B#enqueue()V",
            appCallerLine = 24, workerInstanceId = unsignedWorkerId, runAttempt = 0, generation = 4),
            bits(3, 11), BinaryPayload().uvarint(22).boundedString("a.B#enqueue()V", 512)
                .uvarint(24).uvarint(unsignedWorkerId).uvarint(0).uvarint(4))
        verify(CausalEventObservation(CausalKind.RESOURCE, CausalPhase.Resource.SCOPE_TRANSFERRED, 5L,
            observedNanos = 6L, resourceKind = 2, leaseScopeKnown = true),
            bits(12, 18), BinaryPayload().uvarint(2).uvarint(0).uvarint(0).uvarint(0))
        verify(CausalEventObservation(CausalKind.HANDLER_POST, CausalPhase.HandlerPost.POST_RETURNED, 6L,
            observedNanos = 7L, callResult = 1), bits(14), BinaryPayload().uvarint(1))
        verify(CausalEventObservation(CausalKind.WORKER_ATTEMPT, CausalPhase.WorkerAttempt.TERMINAL_OBSERVED, 7L,
            observedNanos = 8L, terminalOutcome = 4), bits(15), BinaryPayload().uvarint(4))
        verify(CausalEventObservation(CausalKind.SERIES, CausalPhase.Series.DECLARED, 8L,
            observedNanos = 9L, requestedPeriodNanos = 100L, scheduleCadence = 2),
            bits(19, 20), BinaryPayload().uvarint(100).uvarint(2))
        verify(CausalEventObservation(CausalKind.HANDLER_POST, CausalPhase.HandlerPost.POST_INVOKED, 9L,
            observedNanos = 10L, requestedDelayNanos = 0L), bits(21), BinaryPayload().uvarint(0))
        assertEquals((1L shl 22) - 1L, covered)
    }

    @Test
    fun unknownValuesStayAbsentWhileMeasuredZeroStaysPresent() {
        val cost = CausalEventObservation(CausalKind.OPERATION, 1, 1L,
            observedNanos = 2L, operationKind = 1, durationNanos = 0L)
        assertTail(cost, bits(5, 13), BinaryPayload().uvarint(0).uvarint(1))
        assertTail(cost.copy(readBytes = 0L, writtenBytes = 0L), bits(5, 6, 7, 13),
            BinaryPayload().uvarint(0).uvarint(0).uvarint(0).uvarint(1))
        val segment = CausalEventObservation(CausalKind.COROUTINE, CausalPhase.Coroutine.SUSPENDED, 2L,
            observedNanos = 3L, targetTid = 7L)
        assertTail(segment, bits(1), BinaryPayload().uvarint(7))
        assertTail(segment.copy(cpuNanos = 0L, durationNanos = 0L), bits(1, 4, 5),
            BinaryPayload().uvarint(7).uvarint(0).uvarint(0))
        val post = CausalEventObservation(CausalKind.HANDLER_POST, CausalPhase.HandlerPost.POST_INVOKED,
            3L, observedNanos = 4L)
        assertTail(post, 0L, BinaryPayload())
        assertTail(post.copy(requestedDelayNanos = 0L), bits(21), BinaryPayload().uvarint(0))
    }

    @Test
    fun kindSpecificFieldsAndRequiredPairsRejectInvalidPayloads() {
        val operation = CausalEventObservation(CausalKind.OPERATION, 1, 1L,
            observedNanos = 2L, operationKind = 1, durationNanos = 0L)
        listOf(
            operation.copy(resourceKind = 1),
            operation.copy(sourceMethod = "not verified", sourceOrigin = 3),
            operation.copy(requestedDelayNanos = 0L),
            operation.copy(leaseScopeKnown = true),
            CausalEventObservation(CausalKind.WORKER_ATTEMPT, CausalPhase.WorkerAttempt.BOUND,
                1L, observedNanos = 2L),
            CausalEventObservation(CausalKind.SERIES, CausalPhase.Series.DECLARED,
                1L, observedNanos = 2L, scheduleCadence = 2),
            CausalEventObservation(CausalKind.DIRECT_EXECUTION, CausalPhase.DirectExecution.RUN_STARTED,
                1L, schedulerKind = CausalKind.EXECUTOR, schedulerId = 2L, observedNanos = 3L),
        ).forEach { invalid ->
            assertThrows(IllegalArgumentException::class.java) { encodeCausalEvent(BinaryPayload(), invalid) }
        }
        assertTail(CausalEventObservation(CausalKind.COROUTINE, CausalPhase.Coroutine.STATE_MACHINE_OBSERVED,
            1L, observedNanos = 2L), 0L, BinaryPayload())
        assertTail(CausalEventObservation(CausalKind.RESOURCE, CausalPhase.Resource.USE_ENTERED,
            1L, observedNanos = 2L, resourceKind = 1), bits(12), BinaryPayload().uvarint(1))
    }

    @Test
    fun handlerRequestedDelayPreservesKnownZeroOnlyOnPostInvocation() {
        val unknown = BinaryPayload()
        val knownZero = BinaryPayload()
        val post = CausalEventObservation(
            kind = CausalKind.HANDLER_POST, phase = CausalPhase.HandlerPost.POST_INVOKED,
            id = 1L, observedNanos = 2L,
        )
        encodeCausalEvent(unknown, post)
        encodeCausalEvent(knownZero, post.copy(requestedDelayNanos = 0L))
        assertTrue(knownZero.size > unknown.size)
        assertThrows(IllegalArgumentException::class.java) {
            encodeCausalEvent(BinaryPayload(), post.copy(
                phase = CausalPhase.HandlerPost.DISPATCH_STARTED, requestedDelayNanos = 1L,
            ))
        }
    }

    @Test
    fun explicitResourceTransferOutsideScopeHasAValidKnownEmptyScope() {
        val payload = BinaryPayload()
        encodeCausalEvent(payload, CausalEventObservation(
            kind = CausalKind.RESOURCE, phase = CausalPhase.Resource.SCOPE_TRANSFERRED,
            id = 1L, observedNanos = 2L, resourceKind = 2, leaseScopeKnown = true,
        ))
        assertTrue(payload.size > 13)

        assertThrows(IllegalArgumentException::class.java) {
            encodeCausalEvent(BinaryPayload(), CausalEventObservation(
                kind = CausalKind.RESOURCE, phase = CausalPhase.Resource.ACQUIRE_ATTEMPT,
                id = 1L, observedNanos = 2L, resourceKind = 2, leaseScopeKnown = true,
            ))
        }
    }

    @Test
    fun cpuAndOperationCostNeedDirectEvidence() {
        assertThrows(IllegalArgumentException::class.java) {
            encodeCausalEvent(BinaryPayload(), CausalEventObservation(
                kind = CausalKind.DIRECT_EXECUTION, phase = CausalPhase.DirectExecution.RUN_ENDED,
                id = 1L, observedNanos = 2L, cpuNanos = 3L,
            ))
        }
        assertThrows(IllegalArgumentException::class.java) {
            encodeCausalEvent(BinaryPayload(), CausalEventObservation(
                kind = CausalKind.OPERATION, phase = 1, id = 1L, observedNanos = 2L,
                operationKind = 5,
            ))
        }
    }

    private fun assertTail(event: CausalEventObservation, expectedPresence: Long, expectedTail: BinaryPayload) {
        val payload = BinaryPayload()
        encodeCausalEvent(payload, event)
        val bytes = payload.copyBytes()
        var cursor = 0
        repeat(12) { cursor = uvarint(bytes, cursor).second }
        val (presence, tailStart) = uvarint(bytes, cursor)
        assertEquals(expectedPresence, presence)
        assertEquals(0L, presence and ((1L shl 22) - 1L).inv())
        assertArrayEquals(expectedTail.copyBytes(), bytes.copyOfRange(tailStart, bytes.size))
    }

    private fun uvarint(bytes: ByteArray, start: Int): Pair<Long, Int> {
        var value = 0L
        var shift = 0
        var offset = start
        while (offset < bytes.size && shift < Long.SIZE_BITS) {
            val byte = bytes[offset++].toInt() and 0xff
            value = value or ((byte and 0x7f).toLong() shl shift)
            if (byte and 0x80 == 0) return value to offset
            shift += 7
        }
        error("Truncated causal uvarint")
    }

    private fun bits(vararg indices: Int): Long = indices.fold(0L) { mask, index -> mask or (1L shl index) }
}
