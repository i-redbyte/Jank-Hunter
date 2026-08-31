package io.jankhunter.workmanager

import android.content.Context
import androidx.work.ListenableWorker
import androidx.work.OneTimeWorkRequest
import androidx.work.PeriodicWorkRequest
import androidx.work.Worker
import androidx.work.WorkerParameters
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.runtime.JankHunterWorkerOutcome
import java.lang.reflect.Modifier
import java.util.UUID
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkManagerIntegrationTest {
    @Test
    fun enqueueBatchRetainsOnlyPrivatePrimitiveIdentityAndPeriodicity() {
        val oneTimeId = UUID(0x1234L, 0x5678L)
        val periodicId = UUID(0x2234L, 0x6678L)
        val oneTime = OneTimeWorkRequest.Builder(TestWorker::class.java).setId(oneTimeId).build()
        val periodic = PeriodicWorkRequest.Builder(
            TestWorker::class.java,
            PeriodicWorkRequest.MIN_PERIODIC_INTERVAL_MILLIS,
            TimeUnit.MILLISECONDS,
        ).setId(periodicId).build()

        val batch = WorkEnqueueBatch.capture(listOf(oneTime, periodic))

        assertEquals(2, batch.size)
        assertEquals(
            JankHunterWorkerRuntime.instanceId(oneTimeId.mostSignificantBits, oneTimeId.leastSignificantBits),
            batch.instanceIdAt(0),
        )
        assertFalse(batch.periodicAt(0))
        assertEquals(
            JankHunterWorkerRuntime.instanceId(periodicId.mostSignificantBits, periodicId.leastSignificantBits),
            batch.instanceIdAt(1),
        )
        assertTrue(batch.periodicAt(1))
    }

    @Test
    fun enqueueListenerPublishesOnlyAfterSuccessfulOperation() {
        val accepted = AtomicInteger()
        val sink = WorkerEnqueueSink { _, _ -> accepted.incrementAndGet() }
        val batch = WorkEnqueueBatch.capture(
            listOf(OneTimeWorkRequest.Builder(TestWorker::class.java).build()),
        )

        WorkEnqueueListener(CompletableFuture.completedFuture(Unit), batch, sink).run()
        val failed = CompletableFuture<Unit>().also { it.completeExceptionally(IllegalStateException("rejected")) }
        WorkEnqueueListener(failed, batch, sink).run()
        val cancelled = CompletableFuture<Unit>().also { it.cancel(false) }
        WorkEnqueueListener(cancelled, batch, sink).run()

        assertEquals(1, accepted.get())
    }

    @Test
    fun workManagerResultsHaveExactOutcomes() {
        assertEquals(JankHunterWorkerOutcome.SUCCESS, WorkerTelemetry.outcome(ListenableWorker.Result.success()))
        assertEquals(JankHunterWorkerOutcome.FAILURE, WorkerTelemetry.outcome(ListenableWorker.Result.failure()))
        assertEquals(JankHunterWorkerOutcome.RETRY, WorkerTelemetry.outcome(ListenableWorker.Result.retry()))
    }

    @Test
    fun lifecycleBaseClassesOwnFinalDoWorkBoundaries() {
        val workerBoundary = JankHunterWorker::class.java.getDeclaredMethod("doWork")
        val coroutineBoundary = JankHunterCoroutineWorker::class.java.declaredMethods.single { method ->
            method.name == "doWork"
        }
        val coroutineWorkMethods = JankHunterCoroutineWorker::class.java.declaredMethods.filter { method ->
            method.name == "doJankHunterWork"
        }

        assertTrue(Modifier.isFinal(workerBoundary.modifiers))
        assertTrue(Modifier.isFinal(coroutineBoundary.modifiers))
        assertTrue(
            Modifier.isAbstract(
                JankHunterWorker::class.java.getDeclaredMethod("doJankHunterWork").modifiers,
            ),
        )
        assertTrue(coroutineWorkMethods.isNotEmpty())
        assertTrue(coroutineWorkMethods.all { method -> Modifier.isAbstract(method.modifiers) })
    }

    private class TestWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result = Result.success()
    }
}
