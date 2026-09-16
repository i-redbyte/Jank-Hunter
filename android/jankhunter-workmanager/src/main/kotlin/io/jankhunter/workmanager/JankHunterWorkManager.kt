package io.jankhunter.workmanager

import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequest
import androidx.work.Operation
import androidx.work.PeriodicWorkRequest
import androidx.work.WorkManager
import androidx.work.WorkInfo
import androidx.work.WorkRequest
import com.google.common.util.concurrent.ListenableFuture
import io.jankhunter.runtime.JankHunterWorkerRuntime
import java.util.UUID
import java.util.concurrent.Executor
import java.util.concurrent.Future
import java.util.concurrent.atomic.AtomicInteger

/** Enqueues one request and asynchronously records observed registration of its UUID. */
fun WorkManager.enqueueWithJankHunter(request: WorkRequest): Operation =
    enqueue(request).trackIfActive(this) { WorkEnqueueBatch.capture(request) }

/** Enqueues requests without retaining their input, output or tags while WorkManager commits them. */
fun WorkManager.enqueueWithJankHunter(requests: List<WorkRequest>): Operation =
    enqueue(requests).trackIfActive(this) { WorkEnqueueBatch.capture(requests) }

fun WorkManager.enqueueUniqueWorkWithJankHunter(
    uniqueWorkName: String,
    existingWorkPolicy: ExistingWorkPolicy,
    request: OneTimeWorkRequest,
): Operation = enqueueUniqueWork(uniqueWorkName, existingWorkPolicy, request)
    .trackIfActive(this) { WorkEnqueueBatch.capture(request) }

fun WorkManager.enqueueUniqueWorkWithJankHunter(
    uniqueWorkName: String,
    existingWorkPolicy: ExistingWorkPolicy,
    requests: List<OneTimeWorkRequest>,
): Operation = enqueueUniqueWork(uniqueWorkName, existingWorkPolicy, requests)
    .trackIfActive(this) { WorkEnqueueBatch.capture(requests) }

fun WorkManager.enqueueUniquePeriodicWorkWithJankHunter(
    uniqueWorkName: String,
    existingPeriodicWorkPolicy: ExistingPeriodicWorkPolicy,
    request: PeriodicWorkRequest,
): Operation = enqueueUniquePeriodicWork(uniqueWorkName, existingPeriodicWorkPolicy, request)
    .trackIfActive(this) { WorkEnqueueBatch.capture(request) }

private inline fun Operation.trackIfActive(manager: WorkManager, capture: () -> WorkEnqueueBatch): Operation {
    if (!JankHunterWorkerRuntime.isActive()) return this
    return try {
        track(capture(), WorkRegistrationLookup(manager::getWorkInfoById))
    } catch (_: Throwable) {
        this
    }
}

private fun Operation.track(batch: WorkEnqueueBatch, lookup: WorkRegistrationLookup): Operation {
    try {
        val completion = result
        completion.addListener(
            WorkEnqueueListener(completion, batch, RuntimeWorkerEnqueueSink, lookup),
            DirectExecutor,
        )
    } catch (_: Throwable) {
        // Telemetry is fail-open: WorkManager already owns the returned operation.
    }
    return this
}

internal class WorkEnqueueBatch private constructor(private val entries: LongArray) {
    val size: Int
        get() = entries.size / ENTRY_WIDTH

    fun instanceIdAt(index: Int): Long = entries[index * ENTRY_WIDTH]

    fun periodicAt(index: Int): Boolean = entries[index * ENTRY_WIDTH + PERIODIC_OFFSET] != 0L

    fun uuidAt(index: Int): UUID = UUID(
        entries[index * ENTRY_WIDTH + UUID_MOST_OFFSET],
        entries[index * ENTRY_WIDTH + UUID_LEAST_OFFSET],
    )

    fun matchesUuidAt(index: Int, id: UUID): Boolean =
        entries[index * ENTRY_WIDTH + UUID_MOST_OFFSET] == id.mostSignificantBits &&
            entries[index * ENTRY_WIDTH + UUID_LEAST_OFFSET] == id.leastSignificantBits

    companion object {
        fun capture(request: WorkRequest): WorkEnqueueBatch {
            val entries = LongArray(ENTRY_WIDTH)
            capture(request, entries, 0)
            return WorkEnqueueBatch(entries)
        }

        fun capture(requests: List<WorkRequest>): WorkEnqueueBatch {
            val entries = LongArray(requests.size * ENTRY_WIDTH)
            requests.forEachIndexed { index, request -> capture(request, entries, index * ENTRY_WIDTH) }
            return WorkEnqueueBatch(entries)
        }

        private fun capture(request: WorkRequest, entries: LongArray, offset: Int) {
            entries[offset] = WorkerTelemetry.instanceId(request.id)
            entries[offset + UUID_MOST_OFFSET] = request.id.mostSignificantBits
            entries[offset + UUID_LEAST_OFFSET] = request.id.leastSignificantBits
            entries[offset + PERIODIC_OFFSET] = if (request is PeriodicWorkRequest) 1L else 0L
        }

        private const val ENTRY_WIDTH = 4
        private const val UUID_MOST_OFFSET = 2
        private const val UUID_LEAST_OFFSET = 3
        private const val PERIODIC_OFFSET = 1
    }
}

internal fun interface WorkerEnqueueSink {
    fun accepted(instanceId: Long, periodic: Boolean)
}

internal fun interface WorkRegistrationLookup {
    fun lookup(id: UUID): ListenableFuture<WorkInfo?>
}

/** Serial trampoline: ready futures cannot recurse, and at most one UUID query is in flight. */
internal class WorkEnqueueListener(
    completion: Future<*>,
    batch: WorkEnqueueBatch,
    sink: WorkerEnqueueSink,
    lookup: WorkRegistrationLookup,
) : Runnable {
    private val work = AtomicInteger()
    private var completion: Future<*>? = completion
    private var batch: WorkEnqueueBatch? = batch
    private var sink: WorkerEnqueueSink? = sink
    private var lookup: WorkRegistrationLookup? = lookup
    private var pending: ListenableFuture<WorkInfo?>? = null
    private var index = 0

    override fun run() {
        if (work.getAndIncrement() != 0) return
        var missed = 1
        do {
            try {
                drainReady()
            } catch (_: Throwable) {
                release()
            }
            missed = work.addAndGet(-missed)
        } while (missed != 0)
    }

    private fun drainReady() {
        completion?.let { operation ->
            if (!operation.isDone) return
            try {
                operation.get()
            } catch (interrupted: InterruptedException) {
                Thread.currentThread().interrupt()
                release()
                return
            } catch (_: Throwable) {
                release()
                return
            }
            completion = null
        }
        val entries = batch ?: return
        while (index < entries.size) {
            val query = pending ?: startQuery(entries) ?: continue
            if (!query.isDone) return
            pending = null
            val current = index++
            try {
                val observed = query.get()
                if (observed != null && entries.matchesUuidAt(current, observed.id)) {
                    sink?.accepted(entries.instanceIdAt(current), entries.periodicAt(current))
                }
            } catch (interrupted: InterruptedException) {
                Thread.currentThread().interrupt()
                release()
                return
            } catch (_: Throwable) {
                // An absent or failed observation is not evidence of registration.
            }
        }
        release()
    }

    private fun startQuery(entries: WorkEnqueueBatch): ListenableFuture<WorkInfo?>? {
        return try {
            val query = lookup!!.lookup(entries.uuidAt(index))
            pending = query
            query.addListener(this, DirectExecutor)
            query
        } catch (interrupted: InterruptedException) {
            Thread.currentThread().interrupt()
            throw interrupted
        } catch (_: Throwable) {
            pending = null
            index++
            null
        }
    }

    private fun release() {
        completion = null
        pending = null
        batch = null
        sink = null
        lookup = null
    }
}

private object RuntimeWorkerEnqueueSink : WorkerEnqueueSink {
    override fun accepted(instanceId: Long, periodic: Boolean) {
        JankHunterWorkerRuntime.registeredObserved(instanceId, periodic)
    }
}

private object DirectExecutor : Executor {
    override fun execute(command: Runnable) = command.run()
}
