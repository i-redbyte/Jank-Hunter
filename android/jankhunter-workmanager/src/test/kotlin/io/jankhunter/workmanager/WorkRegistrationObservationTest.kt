package io.jankhunter.workmanager

import android.content.Context
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkInfo
import androidx.work.Worker
import androidx.work.WorkerParameters
import com.google.common.util.concurrent.ListenableFuture
import java.util.UUID
import java.util.concurrent.CompletableFuture
import java.util.concurrent.Executor
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import java.lang.reflect.Modifier
import org.junit.Test

class WorkRegistrationObservationTest {
    @Test
    fun successfulOperationDoesNotPublishAbsentUUID() {
        val request = request()
        var accepted = 0
        val queried = mutableListOf<UUID>()
        val listener = WorkEnqueueListener(
            CompletableFuture.completedFuture(Unit), WorkEnqueueBatch.capture(request),
            WorkerEnqueueSink { _, _ -> accepted++ },
            WorkRegistrationLookup { id -> queried += id; ObservationFuture().apply { complete(null) } },
        )
        listener.run()
        assertEquals(listOf(request.id), queried)
        assertEquals(0, accepted)
    }

    @Test
    fun pendingQueryReturnsImmediatelyAndDoesNotStartTheNextQuery() {
        val first = request()
        val second = request()
        val pending = ObservationFuture()
        val queried = mutableListOf<UUID>()
        var accepted = 0
        val listener = WorkEnqueueListener(
            CompletableFuture.completedFuture(Unit), WorkEnqueueBatch.capture(listOf(first, second)),
            WorkerEnqueueSink { _, _ -> accepted++ },
            WorkRegistrationLookup { id ->
                queried += id
                if (id == first.id) pending else ObservationFuture().apply { complete(registered(id)) }
            },
        )
        listener.run()
        assertEquals(listOf(first.id), queried)
        assertEquals(0, accepted)
        assertFalse(pending.blockingGetAttempted)
        pending.complete(registered(first.id))
        assertEquals(listOf(first.id, second.id), queried)
        assertEquals(2, accepted)
    }

    @Test
    fun longBatchOfAlreadyCompletedQueriesUsesBoundedCallStack() {
        val requests = List(10_000) { request() }
        var accepted = 0
        var queries = 0
        WorkEnqueueListener(
            CompletableFuture.completedFuture(Unit), WorkEnqueueBatch.capture(requests),
            WorkerEnqueueSink { _, _ -> accepted++ },
            WorkRegistrationLookup { id -> queries++; ObservationFuture().apply { complete(registered(id)) } },
        ).run()
        assertEquals(requests.size, queries)
        assertEquals(requests.size, accepted)
    }

    @Test
    fun incompleteOperationDoesNotCallBlockingGet() {
        var blockingGet = false
        val pending = object : Future<Unit> {
            override fun isDone(): Boolean = false
            override fun isCancelled(): Boolean = false
            override fun cancel(mayInterruptIfRunning: Boolean): Boolean = false
            override fun get() { blockingGet = true; throw IllegalStateException("would block") }
            override fun get(timeout: Long, unit: TimeUnit) = get()
        }
        var queried = false
        WorkEnqueueListener(pending, WorkEnqueueBatch.capture(request()), WorkerEnqueueSink { _, _ ->
            throw AssertionError("unconfirmed operation")
        }, WorkRegistrationLookup { queried = true; ObservationFuture() }).run()
        assertFalse(blockingGet)
        assertFalse(queried)
    }

    @Test
    fun failedCancelledAndWrongUUIDQueriesDoNotPublishAndBatchContinues() {
        val requests = List(5) { request() }
        val observed = mutableListOf<Long>()
        var index = 0
        val listener = WorkEnqueueListener(
            CompletableFuture.completedFuture(Unit), WorkEnqueueBatch.capture(requests),
            WorkerEnqueueSink { id, _ -> observed += id },
            WorkRegistrationLookup { id ->
                when (index++) {
                    0 -> throw IllegalStateException("query dispatch failed")
                    1 -> ObservationFuture().apply { completeExceptionally(IllegalStateException("database closed")) }
                    2 -> ObservationFuture().apply { cancel(false) }
                    3 -> ObservationFuture().apply { complete(registered(UUID.randomUUID())) }
                    else -> ObservationFuture().apply { complete(registered(id)) }
                }
            },
        )
        listener.run()
        listener.run()
        assertEquals(5, index)
        assertEquals(listOf(WorkerTelemetry.instanceId(requests.last().id)), observed)
        for (field in WorkEnqueueListener::class.java.declaredFields) {
            if (!Modifier.isStatic(field.modifiers) && !field.type.isPrimitive && field.name != "work") {
                field.isAccessible = true
                assertNull("completed listener retained ${field.name}", field.get(listener))
            }
        }
    }

    @Test
    fun duplicateCompletionCallbacksNeverDuplicateObservations() {
        val request = request()
        val pending = ObservationFuture()
        var accepted = 0
        val listener = WorkEnqueueListener(
            CompletableFuture.completedFuture(Unit), WorkEnqueueBatch.capture(request),
            WorkerEnqueueSink { _, _ -> accepted++ }, WorkRegistrationLookup { pending },
        )
        listener.run()
        val threads = List(4) { Thread { repeat(1000) { listener.run() } } }
        threads.forEach { it.start() }
        pending.complete(registered(request.id))
        threads.forEach { it.join(5000L); assertFalse(it.isAlive) }
        assertEquals(1, accepted)
    }

    private class ObservationFuture : CompletableFuture<WorkInfo?>(), ListenableFuture<WorkInfo?> {
        var blockingGetAttempted = false
        override fun addListener(listener: Runnable, executor: Executor) {
            whenComplete { _, _ -> executor.execute(listener) }
        }
        override fun get(): WorkInfo? {
            if (!isDone) {
                blockingGetAttempted = true
                throw AssertionError("observation get would block")
            }
            return super.get()
        }
    }

    private fun request() = OneTimeWorkRequest.Builder(TestWorker::class.java).build()
    private fun registered(id: UUID) = WorkInfo(id, WorkInfo.State.ENQUEUED, emptySet())

    private class TestWorker(context: Context, parameters: WorkerParameters) : Worker(context, parameters) {
        override fun doWork(): Result = Result.success()
    }
}
