package io.jankhunter.workmanager

import android.content.Context
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.runtime.JankHunterWorkerOutcome
import java.util.concurrent.CancellationException

/** CoroutineWorker base whose lifecycle boundary follows the coroutine through suspension. */
abstract class JankHunterCoroutineWorker(
    appContext: Context,
    workerParameters: WorkerParameters,
) : CoroutineWorker(appContext, workerParameters) {
    private val workerGeneration = workerParameters.generation

    final override suspend fun doWork(): Result {
        if (!JankHunterWorkerRuntime.isActive()) return doJankHunterWork()
        val instanceId = WorkerTelemetry.instanceId(id)
        val workerName = javaClass.name
        val attempt = runAttemptCount
        val token = JankHunterWorkerRuntime.started(instanceId, workerName, attempt, workerGeneration)
        if (token == 0L) return doJankHunterWork()
        var outcome = JankHunterWorkerOutcome.UNKNOWN
        try {
            val result = doJankHunterWork()
            outcome = WorkerTelemetry.outcome(result)
            return result
        } catch (throwable: Throwable) {
            outcome = if (throwable is CancellationException) {
                JankHunterWorkerOutcome.CANCELLED
            } else {
                JankHunterWorkerOutcome.FAILURE
            }
            throw throwable
        } finally {
            WorkerTelemetry.finish(this, token, instanceId, workerName, outcome, attempt, workerGeneration)
        }
    }

    /** Implements suspending application work while [doWork] remains the telemetry boundary. */
    protected abstract suspend fun doJankHunterWork(): Result
}
