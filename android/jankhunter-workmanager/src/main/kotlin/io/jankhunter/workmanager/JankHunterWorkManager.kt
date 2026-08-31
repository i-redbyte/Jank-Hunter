package io.jankhunter.workmanager

import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequest
import androidx.work.Operation
import androidx.work.PeriodicWorkRequest
import androidx.work.WorkManager
import androidx.work.WorkRequest
import io.jankhunter.runtime.JankHunterWorkerRuntime
import java.util.concurrent.Executor
import java.util.concurrent.Future

/** Enqueues one request and records ENQUEUED only after WorkManager accepts the operation. */
fun WorkManager.enqueueWithJankHunter(request: WorkRequest): Operation =
    enqueue(request).trackIfActive { WorkEnqueueBatch.capture(request) }

/** Enqueues requests without retaining their input, output or tags while WorkManager commits them. */
fun WorkManager.enqueueWithJankHunter(requests: List<WorkRequest>): Operation =
    enqueue(requests).trackIfActive { WorkEnqueueBatch.capture(requests) }

fun WorkManager.enqueueUniqueWorkWithJankHunter(
    uniqueWorkName: String,
    existingWorkPolicy: ExistingWorkPolicy,
    request: OneTimeWorkRequest,
): Operation = enqueueUniqueWork(uniqueWorkName, existingWorkPolicy, request)
    .trackIfActive { WorkEnqueueBatch.capture(request) }

fun WorkManager.enqueueUniqueWorkWithJankHunter(
    uniqueWorkName: String,
    existingWorkPolicy: ExistingWorkPolicy,
    requests: List<OneTimeWorkRequest>,
): Operation = enqueueUniqueWork(uniqueWorkName, existingWorkPolicy, requests)
    .trackIfActive { WorkEnqueueBatch.capture(requests) }

fun WorkManager.enqueueUniquePeriodicWorkWithJankHunter(
    uniqueWorkName: String,
    existingPeriodicWorkPolicy: ExistingPeriodicWorkPolicy,
    request: PeriodicWorkRequest,
): Operation = enqueueUniquePeriodicWork(uniqueWorkName, existingPeriodicWorkPolicy, request)
    .trackIfActive { WorkEnqueueBatch.capture(request) }

private inline fun Operation.trackIfActive(capture: () -> WorkEnqueueBatch): Operation {
    if (!JankHunterWorkerRuntime.isActive()) return this
    return try {
        track(capture())
    } catch (_: Throwable) {
        this
    }
}

private fun Operation.track(batch: WorkEnqueueBatch): Operation {
    try {
        val completion = result
        completion.addListener(
            WorkEnqueueListener(completion, batch, RuntimeWorkerEnqueueSink),
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

    fun publishTo(sink: WorkerEnqueueSink) {
        var offset = 0
        while (offset < entries.size) {
            sink.accepted(entries[offset], entries[offset + PERIODIC_OFFSET] != 0L)
            offset += ENTRY_WIDTH
        }
    }

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
            entries[offset + PERIODIC_OFFSET] = if (request is PeriodicWorkRequest) 1L else 0L
        }

        private const val ENTRY_WIDTH = 2
        private const val PERIODIC_OFFSET = 1
    }
}

internal fun interface WorkerEnqueueSink {
    fun accepted(instanceId: Long, periodic: Boolean)
}

internal class WorkEnqueueListener(
    private val completion: Future<*>,
    private val batch: WorkEnqueueBatch,
    private val sink: WorkerEnqueueSink,
) : Runnable {
    override fun run() {
        try {
            completion.get()
        } catch (interrupted: InterruptedException) {
            Thread.currentThread().interrupt()
            return
        } catch (_: Throwable) {
            return
        }
        batch.publishTo(sink)
    }
}

private object RuntimeWorkerEnqueueSink : WorkerEnqueueSink {
    override fun accepted(instanceId: Long, periodic: Boolean) {
        JankHunterWorkerRuntime.enqueued(instanceId, periodic)
    }
}

private object DirectExecutor : Executor {
    override fun execute(command: Runnable) = command.run()
}
