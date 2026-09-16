package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CoroutineExecutionTrackerTest {
    @Test
    fun separatesActiveAndSuspendedTimeAcrossThreadMigration() {
        val clock = AtomicLong(100L)
        val threadId = AtomicLong(1L)
        val completed = mutableListOf<CompletedExecution>()
        val tracker = tracker(clock, threadId, completed)
        val continuation = Any()

        val firstToken = tracker.enter(continuation, "example.Repository.load", collectNew = true)
        clock.addAndGet(20L)
        tracker.exit(firstToken, continuation, suspended = true, CoroutineExecutionOutcome.SUCCESS)
        clock.addAndGet(50L)
        threadId.set(2L)
        val resumedToken = tracker.enter(continuation, "ignored.owner", collectNew = true)
        clock.addAndGet(30L)
        tracker.exit(resumedToken, continuation, suspended = false, CoroutineExecutionOutcome.SUCCESS)

        assertEquals(firstToken, resumedToken)
        assertEquals(
            CompletedExecution(
                owner = "example.Repository.load",
                activeDurationMs = 50L,
                suspendedDurationMs = 50L,
                suspensionCount = 1,
                threadMigrationCount = 1,
                generation = 0L,
                outcome = CoroutineExecutionOutcome.SUCCESS,
            ),
            completed.single(),
        )
        assertEquals(0, tracker.retainedEntryCount())
    }

    @Test
    fun existingCoroutineCanFinishAfterCollectionWasDisabled() {
        val clock = AtomicLong(0L)
        val completed = mutableListOf<CompletedExecution>()
        val tracker = tracker(clock, AtomicLong(1L), completed)
        val continuation = Any()

        val token = tracker.enter(continuation, "owner", collectNew = true)
        clock.set(10L)
        tracker.exit(token, continuation, suspended = true, CoroutineExecutionOutcome.SUCCESS)
        clock.set(30L)
        assertEquals(token, tracker.enter(continuation, "owner", collectNew = false))
        clock.set(40L)
        tracker.exit(token, continuation, suspended = false, CoroutineExecutionOutcome.CANCELLED)

        assertEquals(20L, completed.single().activeDurationMs)
        assertEquals(20L, completed.single().suspendedDurationMs)
        assertEquals(CoroutineExecutionOutcome.CANCELLED, completed.single().outcome)
        assertEquals(0L, tracker.enter(Any(), "new", collectNew = false))
    }

    @Test
    fun terminalCompletionKeepsGenerationCapturedByFirstSegment() {
        val clock = AtomicLong(0L)
        val completed = mutableListOf<CompletedExecution>()
        val tracker = tracker(clock, AtomicLong(1L), completed)
        val continuation = Any()

        val token = tracker.enter(continuation, "owner", collectNew = true, generation = 7L)
        tracker.exit(token, continuation, suspended = true, CoroutineExecutionOutcome.SUCCESS)
        tracker.enter(continuation, "owner", collectNew = true, generation = 9L)
        tracker.exit(token, continuation, suspended = false, CoroutineExecutionOutcome.SUCCESS)

        assertEquals(7L, completed.single().generation)
    }

    @Test
    fun duplicateRunningEntryIsRejectedWithoutCorruptingOriginalState() {
        val invalidTransitions = AtomicInteger()
        val clock = AtomicLong(10L)
        val completed = mutableListOf<CompletedExecution>()
        val tracker = CoroutineExecutionTracker(
            capacity = 4,
            shardCount = 1,
            clock = clock::get,
            threadId = { 1L },
            onComplete = sink(completed),
            onInvalidTransition = invalidTransitions::incrementAndGet,
        )
        val continuation = Any()

        val token = tracker.enter(continuation, "owner", collectNew = true)
        assertEquals(0L, tracker.enter(continuation, "owner", collectNew = true))
        clock.set(25L)
        tracker.exit(token, continuation, suspended = false, CoroutineExecutionOutcome.FAILURE)

        assertEquals(1, invalidTransitions.get())
        assertEquals(15L, completed.single().activeDurationMs)
        assertEquals(CoroutineExecutionOutcome.FAILURE, completed.single().outcome)
    }

    @Test
    fun registryBoundsRetainedStateAndReportsEviction() {
        val evictions = AtomicInteger()
        val tracker = CoroutineExecutionTracker(
            capacity = 2,
            shardCount = 1,
            clock = { 0L },
            threadId = { 1L },
            onComplete = CoroutineExecutionSink { _, _, _, _, _, _, _ -> },
            onEviction = evictions::incrementAndGet,
        )
        val continuations = Array(3) { Any() }

        continuations.forEachIndexed { index, continuation ->
            tracker.enter(continuation, "owner$index", collectNew = true)
        }

        assertEquals(1, evictions.get())
        assertTrue(tracker.retainedEntryCount() <= 2)
    }

    @Test
    fun clockRegressionNeverProducesNegativeDurations() {
        val clock = AtomicLong(100L)
        val completed = mutableListOf<CompletedExecution>()
        val tracker = tracker(clock, AtomicLong(1L), completed)
        val continuation = Any()

        val token = tracker.enter(continuation, "owner", collectNew = true)
        clock.set(90L)
        tracker.exit(token, continuation, suspended = false, CoroutineExecutionOutcome.SUCCESS)

        assertEquals(0L, completed.single().activeDurationMs)
    }

    @Test
    fun independentCoroutinesCompleteSafelyFromConcurrentThreads() {
        val completed = AtomicLong()
        val tracker = CoroutineExecutionTracker(
            capacity = 64,
            shardCount = 8,
            clock = System::nanoTime,
            threadId = { Thread.currentThread().id },
            onComplete = CoroutineExecutionSink { _, _, _, _, _, _, _ -> completed.incrementAndGet() },
        )

        val workers = List(WORKER_COUNT) {
            thread(start = true) {
                repeat(COMPLETIONS_PER_WORKER) {
                    val continuation = Any()
                    val token = tracker.enter(continuation, "owner", collectNew = true)
                    tracker.exit(token, continuation, suspended = false, CoroutineExecutionOutcome.SUCCESS)
                }
            }
        }
        workers.forEach(Thread::join)

        assertEquals((WORKER_COUNT * COMPLETIONS_PER_WORKER).toLong(), completed.get())
        assertEquals(0, tracker.retainedEntryCount())
    }

    private fun tracker(
        clock: AtomicLong,
        threadId: AtomicLong,
        completed: MutableList<CompletedExecution>,
    ): CoroutineExecutionTracker = CoroutineExecutionTracker(
        capacity = 4,
        shardCount = 1,
        clock = clock::get,
        threadId = threadId::get,
        onComplete = sink(completed),
    )

    private fun sink(completed: MutableList<CompletedExecution>): CoroutineExecutionSink {
        return CoroutineExecutionSink { owner, active, suspended, suspensions, migrations, generation, outcome ->
            completed += CompletedExecution(owner, active, suspended, suspensions, migrations, generation, outcome)
        }
    }

    private data class CompletedExecution(
        val owner: String,
        val activeDurationMs: Long,
        val suspendedDurationMs: Long,
        val suspensionCount: Int,
        val threadMigrationCount: Int,
        val generation: Long,
        val outcome: CoroutineExecutionOutcome,
    )

    private companion object {
        const val WORKER_COUNT = 8
        const val COMPLETIONS_PER_WORKER = 1_000
    }
}
