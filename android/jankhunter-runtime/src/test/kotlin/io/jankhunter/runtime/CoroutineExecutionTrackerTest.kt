package io.jankhunter.runtime

import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class CoroutineExecutionTrackerTest {
    @Test
    fun unknownJavaIdsUseObservedNativeIdsForMigrationWithoutInventingAJavaId() {
        val tid = AtomicLong(101L)
        val completed = mutableListOf<CompletedExecution>()
        val tracker = CoroutineExecutionTracker(capacity = 4, shardCount = 1,
            clock = { 1L }, threadId = { 0L }, nativeThreadId = tid::get, onComplete = sink(completed))
        val continuation = Any()
        val token = tracker.enter(continuation, "host", true, 1L)
        tracker.exit(token, continuation, true, CoroutineExecutionOutcome.SUCCESS)
        tid.set(202L)
        assertEquals(token, tracker.enter(continuation, "host", true, 1L))
        tracker.exit(token, continuation, false, CoroutineExecutionOutcome.SUCCESS)
        assertEquals(1, completed.single().threadMigrationCount)
    }

    @Test
    fun coroutineEntryNeverWaitsForACollectorShardOwner() {
        val tracker = CoroutineExecutionTracker(capacity = 4, shardCount = 1, clock = { 1L }, threadId = { 1L },
            onComplete = CoroutineExecutionSink { _, _, _, _, _, _, _ -> })
        val field = tracker.javaClass.getDeclaredField("shards").apply { isAccessible = true }
        val shard = (field.get(tracker) as Array<*>)[0]!!
        val lock = try {
            shard.javaClass.getDeclaredField("lock").apply { isAccessible = true }.get(shard) as java.util.concurrent.locks.ReentrantLock
        } catch (missing: NoSuchFieldException) { null }
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val done = CountDownLatch(1)
        val failure = java.util.concurrent.atomic.AtomicReference<Throwable?>()
        val holder = Thread {
            if (lock == null) synchronized(shard) { entered.countDown(); release.await() }
            else { lock.lock(); try { entered.countDown(); release.await() } finally { lock.unlock() } }
        }
        val producer = Thread {
            try { assertEquals(0L, tracker.enter(Any(), "app.Load", true)) }
            catch (error: Throwable) { failure.set(error) }
            finally { done.countDown() }
        }
        try {
            holder.start(); assertTrue(entered.await(5L, TimeUnit.SECONDS)); producer.start()
            assertTrue("coroutine hook waited for diagnostic shard", done.await(100L, TimeUnit.MILLISECONDS))
            assertEquals(null, failure.get())
        } finally { release.countDown(); holder.join(5_000L); producer.join(5_000L); tracker.clear() }
    }

    @Test
    fun captureObservesRunningSegmentWithoutCompletingIt() {
        val clock = AtomicLong(100L)
        val threadId = AtomicLong(11L)
        val nativeThreadId = AtomicLong(1011L)
        val segments = mutableListOf<String>()
        val completed = mutableListOf<CompletedExecution>()
        val tracker = CoroutineExecutionTracker(
            capacity = 4, shardCount = 1, clock = clock::get, threadId = threadId::get,
            nativeThreadId = nativeThreadId::get, onComplete = sink(completed),
            onSegment = CoroutineSegmentSink { _, _, _, _, _, _, _, _, _, _, _ -> segments += "ended" },
        )
        val continuation = Any()
        val token = tracker.enter(continuation, "app.Load", true, 7)
        clock.set(120)
        val running = mutableListOf<RunningCoroutineSegment>()
        tracker.snapshotRunning(running::add)

        assertEquals(listOf(RunningCoroutineSegment(token, 7, "app.Load", 100, 120, 11, 1011)), running)
        assertTrue(segments.isEmpty())
        assertTrue(completed.isEmpty())
        assertEquals(1, tracker.retainedEntryCount())

        clock.set(130)
        tracker.exit(token, continuation, false, CoroutineExecutionOutcome.SUCCESS)
        assertEquals(listOf("ended"), segments)
        assertEquals(30L, completed.single().activeDurationMs)
    }

    @Test
    fun captureExcludesSuspendedSegmentAndObservesNewThreadAfterResume() {
        val clock = AtomicLong(100L)
        val threadId = AtomicLong(11L)
        val nativeThreadId = AtomicLong(1011L)
        val tracker = CoroutineExecutionTracker(
            capacity = 4, shardCount = 1, clock = clock::get, threadId = threadId::get,
            nativeThreadId = nativeThreadId::get, onComplete = sink(mutableListOf()),
        )
        val continuation = Any()
        val token = tracker.enter(continuation, "app.Load", true, 7)
        clock.set(120)
        tracker.exit(token, continuation, true, CoroutineExecutionOutcome.SUCCESS)
        val running = mutableListOf<RunningCoroutineSegment>()
        tracker.snapshotRunning(running::add)
        assertTrue(running.isEmpty())

        clock.set(150)
        threadId.set(22)
        nativeThreadId.set(2022)
        tracker.enter(continuation, "ignored", true, 9)
        clock.set(160)
        tracker.snapshotRunning(running::add)
        assertEquals(listOf(RunningCoroutineSegment(token, 7, "app.Load", 150, 160, 22, 2022)), running)
    }

    @Test
    fun capturePublicationDoesNotHoldShardLockAgainstExit() {
        val tracker = CoroutineExecutionTracker(
            capacity = 4, shardCount = 1, clock = { 100L }, threadId = { 11L },
            onComplete = sink(mutableListOf()),
        )
        val continuation = Any()
        val token = tracker.enter(continuation, "owner", true)
        val publishing = CountDownLatch(1)
        val release = CountDownLatch(1)
        val exited = CountDownLatch(1)
        val capture = thread(start = true) {
            tracker.snapshotRunning {
                publishing.countDown()
                release.await()
            }
        }
        var exit: Thread? = null
        try {
            assertTrue(publishing.await(10, TimeUnit.SECONDS))
            exit = thread(start = true) {
                tracker.exit(token, continuation, false, CoroutineExecutionOutcome.SUCCESS)
                exited.countDown()
            }
            assertTrue(exited.await(10, TimeUnit.SECONDS))
        } finally {
            release.countDown()
            capture.join()
            exit?.join()
        }
    }

    @Test
    fun emitsObservedResumeSegmentsWithDistinctThreadIds() {
        val clock = AtomicLong(100L)
        val threadId = AtomicLong(11L)
        val nativeThreadId = AtomicLong(1011L)
        val segments = mutableListOf<String>()
        val tracker = CoroutineExecutionTracker(
            capacity = 4, shardCount = 1, clock = clock::get, threadId = threadId::get,
            nativeThreadId = nativeThreadId::get,
            onComplete = sink(mutableListOf()),
            onSegment = CoroutineSegmentSink { token, generation, owner, start, end, entry, exit, entryTid, exitTid, suspended, _ ->
                segments += "$token/$generation/$owner/$start/$end/$entry/$exit/$entryTid/$exitTid/$suspended"
            },
        )
        val continuation = Any()
        val token = tracker.enter(continuation, "app.Feed.load", true, 4)
        clock.set(120)
        tracker.exit(token, continuation, true, CoroutineExecutionOutcome.SUCCESS)
        threadId.set(22)
        nativeThreadId.set(2022)
        clock.set(150)
        tracker.enter(continuation, "ignored", true, 5)
        clock.set(180)
        tracker.exit(token, continuation, false, CoroutineExecutionOutcome.SUCCESS)

        assertEquals(listOf("$token/4/app.Feed.load/100/120/11/11/1011/1011/true",
            "$token/4/app.Feed.load/150/180/22/22/2022/2022/false"), segments)
    }

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
        val failuresBefore = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.REGISTRY_CONTENTION)
        val counterBefore = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.COUNTER_CONTENTION)
        val evicted = AtomicLong()
        val tracker = CoroutineExecutionTracker(
            capacity = 64,
            shardCount = 8,
            clock = System::nanoTime,
            threadId = { Thread.currentThread().id },
            onComplete = CoroutineExecutionSink { _, _, _, _, _, _, _ -> completed.incrementAndGet() },
            onEviction = { evicted.incrementAndGet() },
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

        val attempted = (WORKER_COUNT * COMPLETIONS_PER_WORKER).toLong()
        assertTrue(completed.get() in 1..attempted)
        val contention = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.REGISTRY_CONTENTION) - failuresBefore
        val unknownCategory = RuntimeHookFailureTracker.count(RuntimeHookFailureReason.COUNTER_CONTENTION) - counterBefore
        assertTrue("missing observations need contention/eviction evidence: completed=${completed.get()} contention=$contention eviction=${evicted.get()} counter=$unknownCategory",
            completed.get() + contention + evicted.get() + unknownCategory >= attempted)
        // A dropped terminal observation remains running/unknown, never fabricated as completed.
        assertTrue(tracker.retainedEntryCount() <= 64)
        tracker.clear()
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
