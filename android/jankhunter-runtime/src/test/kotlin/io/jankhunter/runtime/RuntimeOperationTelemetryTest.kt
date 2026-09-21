package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import java.util.Collections
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeOperationTelemetryTest {
    @Test
    fun nestedOperationsRestoreParentAndWriteDeterministicLifecycle() {
        val tracker = ContextTracker("Feed")
        val sink = RecordingOperationSink()
        var nowUs = 1_000L
        val telemetry = RuntimeOperationTelemetry(tracker, { sink }, { nowUs }, {})

        val outer = telemetry.start(
            "content.load",
            JankHunterOperationKind.USER,
            250L,
            JankHunterOperationAttributes.of("source", "cache"),
        )
        nowUs = 1_400L
        val inner = telemetry.start(
            "content.decode",
            JankHunterOperationKind.STAGE,
            0L,
            JankHunterOperationAttributes.EMPTY,
        )

        assertEquals(outer.id, inner.parentId)
        assertEquals(inner.id, tracker.currentOperationId())
        assertEquals(inner.id, tracker.capture().operationId)

        nowUs = 2_000L
        assertTrue(inner.failure())
        assertEquals(outer.id, tracker.currentOperationId())
        assertNull(inner.sink)
        assertNull(inner.previousOperation)
        assertSame(JankHunterOperationAttributes.EMPTY, inner.attributes)
        nowUs = 3_500L
        assertTrue(outer.success())
        assertEquals(0L, tracker.currentOperationId())
        assertNull(outer.sink)
        assertSame(JankHunterOperationAttributes.EMPTY, outer.attributes)

        assertEquals(4, sink.records.size)
        assertEquals(Jhlog.OPERATION_PHASE_STARTED, sink.records[0].phase)
        assertEquals(250_000L, sink.records[0].budgetUs)
        assertEquals(Jhlog.OPERATION_PHASE_STARTED, sink.records[1].phase)
        assertEquals(Jhlog.OPERATION_PHASE_FINISHED, sink.records[2].phase)
        assertEquals(JankHunterOperationOutcome.FAILURE.wireValue, sink.records[2].outcome)
        assertEquals(600L, sink.records[2].durationUs)
        assertEquals(Jhlog.OPERATION_PHASE_FINISHED, sink.records[3].phase)
        assertEquals(JankHunterOperationOutcome.SUCCESS.wireValue, sink.records[3].outcome)
        assertEquals(2_500L, sink.records[3].durationUs)
        assertEquals(250_000L, sink.records[3].budgetUs)
        assertSame(sink.records[0].attributes, sink.records[3].attributes)
    }

    @Test
    fun completionIsExactlyOnceAcrossRacingThreads() {
        val sink = RecordingOperationSink()
        val telemetry = RuntimeOperationTelemetry(ContextTracker(), { sink }, { 10_000L }, {})
        val operation = telemetry.start(
            "sync.refresh",
            JankHunterOperationKind.BACKGROUND,
            0L,
            JankHunterOperationAttributes.EMPTY,
        )
        val ready = CountDownLatch(RACING_THREADS)
        val release = CountDownLatch(1)
        val completed = AtomicInteger()
        val workers = List(RACING_THREADS) { index ->
            thread {
                ready.countDown()
                assertTrue(release.await(2, TimeUnit.SECONDS))
                val won = if (index and 1 == 0) operation.success() else operation.failure()
                if (won) completed.incrementAndGet()
            }
        }

        assertTrue(ready.await(2, TimeUnit.SECONDS))
        release.countDown()
        workers.forEach { it.join(2_000L) }

        assertEquals(1, completed.get())
        assertTrue(operation.isFinished)
        assertEquals(1, sink.records.count { it.phase == Jhlog.OPERATION_PHASE_FINISHED })
        assertFalse(operation.success())
    }

    @Test
    fun completionKeepsTheContextCapturedAtOperationStart() {
        val tracker = ContextTracker("StartScreen")
        val startScope = tracker.enterScopedContext(null, "StartOwner")
        val sink = RecordingOperationSink()
        val telemetry = RuntimeOperationTelemetry(tracker, { sink }, { 10_000L }, {})
        val operation = telemetry.start(
            "content.open",
            JankHunterOperationKind.SCREEN,
            0L,
            JankHunterOperationAttributes.EMPTY,
        )
        tracker.exitScopedContext(startScope)
        tracker.setScreen("FinishScreen")
        val completion = thread {
            val finishScope = tracker.enterScopedContext(null, "FinishOwner")
            try {
                assertTrue(operation.success())
            } finally {
                tracker.exitScopedContext(finishScope)
            }
        }
        completion.join(2_000L)

        assertFalse(completion.isAlive)
        assertEquals(2, sink.records.size)
        sink.records.forEach { record ->
            assertEquals("StartScreen", record.screen)
            assertEquals("StartOwner", record.owner)
        }
        assertNull(operation.screen)
        assertNull(operation.owner)
    }

    @Test
    fun unavailableOrRejectingSinkDoesNotLeaveActiveContext() {
        val tracker = ContextTracker()
        val unavailable = RuntimeOperationTelemetry(tracker, { null }, { 0L }, {})
        assertSame(
            JankHunterOperation.NONE,
            unavailable.start("work", JankHunterOperationKind.USER, 0L, JankHunterOperationAttributes.EMPTY),
        )

        val rejecting = RecordingOperationSink(accept = false)
        val rejected = RuntimeOperationTelemetry(tracker, { rejecting }, { 0L }, {}).start(
            "work",
            JankHunterOperationKind.USER,
            0L,
            JankHunterOperationAttributes.EMPTY,
        )

        assertSame(JankHunterOperation.NONE, rejected)
        assertEquals(0L, tracker.currentOperationId())
        assertEquals(1, rejecting.records.size)
    }

    private class RecordingOperationSink(
        private val accept: Boolean = true,
    ) : OperationEventSink {
        val records: MutableList<Record> = Collections.synchronizedList(mutableListOf())

        override fun operation(
            name: String,
            operationId: Long,
            parentId: Long,
            phase: Long,
            kind: Long,
            outcome: Long,
            durationUs: Long,
            budgetUs: Long,
            screen: String?,
            owner: String?,
            attributes: JankHunterOperationAttributes,
        ): Boolean {
            records += Record(operationId, parentId, phase, outcome, durationUs, budgetUs, screen, owner, attributes)
            return accept
        }
    }

    private data class Record(
        val operationId: Long,
        val parentId: Long,
        val phase: Long,
        val outcome: Long,
        val durationUs: Long,
        val budgetUs: Long,
        val screen: String?,
        val owner: String?,
        val attributes: JankHunterOperationAttributes,
    )

    private companion object {
        const val RACING_THREADS = 32
    }
}
