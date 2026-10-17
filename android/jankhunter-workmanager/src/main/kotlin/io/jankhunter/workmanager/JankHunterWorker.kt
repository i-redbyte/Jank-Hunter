package io.jankhunter.workmanager

import android.content.Context
import androidx.work.Worker
import androidx.work.WorkerParameters
import androidx.work.Data
import androidx.work.ForegroundInfo
import com.google.common.util.concurrent.ListenableFuture
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.runtime.JankHunterWorkerOutcome
import java.util.concurrent.CancellationException

/** WorkManager [Worker] base that records generation and stop-reason-aware lifecycle evidence. */
abstract class JankHunterWorker(
    appContext: Context,
    workerParameters: WorkerParameters,
) : Worker(appContext, workerParameters) {
    private val workerGeneration = workerParameters.generation
    @Volatile private var progressCapture: WorkerProgressCapture? = null
    @Volatile private var foregroundCapture: WorkerForegroundCapture? = null

    /** Use this in place of [setForegroundAsync] to observe a confirmed transition. */
    protected fun setJankHunterForegroundAsync(info: ForegroundInfo): ListenableFuture<Void> {
        val result = setForegroundAsync(info)
        try { foregroundCapture?.observe(result) } catch (_: Exception) { /* Preserve WorkManager behavior. */ }
        return result
    }

    override fun setProgressAsync(data: Data): ListenableFuture<Void> {
        val result = super.setProgressAsync(data)
        try { progressCapture?.request(data) } catch (_: Exception) { /* Telemetry must not change WorkManager. */ }
        return result
    }

    final override fun doWork(): Result {
        if (!JankHunterWorkerRuntime.isActive() && !JankHunterWorkerRuntime.isConcurrencyActive()) {
            return doJankHunterWork()
        }
        val instanceId = WorkerTelemetry.instanceId(id)
        val workerName = javaClass.name
        val attempt = runAttemptCount
        val token = JankHunterWorkerRuntime.started(instanceId, workerName, attempt, workerGeneration)
        val causalContext = JankHunterWorkerRuntime.beginConcurrencyWorker(
            this, instanceId, attempt, workerGeneration, id.mostSignificantBits, id.leastSignificantBits,
        )
        if (token == 0L && causalContext == null) return doJankHunterWork()
        progressCapture = if (token == 0L) null else WorkerProgressCapture().also {
            it.start(token, instanceId, attempt, workerGeneration)
        }
        foregroundCapture = if (token == 0L) null else WorkerForegroundCapture { entered ->
            JankHunterWorkerRuntime.foreground(token, instanceId, workerName, attempt, workerGeneration, entered)
        }
        var outcome = JankHunterWorkerOutcome.UNKNOWN
        val previousContext = causalContext?.let(JankHunterWorkerRuntime::installConcurrencyContext)
        try {
            val result = doJankHunterWork()
            outcome = WorkerTelemetry.outcome(result)
            return result
        } catch (throwable: Throwable) {
            if (token != 0L) JankHunterWorkerRuntime.failed(token, instanceId, attempt, workerGeneration, throwable)
            outcome = if (throwable is CancellationException) {
                JankHunterWorkerOutcome.CANCELLED
            } else {
                JankHunterWorkerOutcome.FAILURE
            }
            throw throwable
        } finally {
            try {
                progressCapture?.finish()
                progressCapture = null
                foregroundCapture?.finish()
                foregroundCapture = null
                val terminalOutcome = WorkerTelemetry.finish(
                    this, token, instanceId, workerName, outcome, attempt, workerGeneration,
                )
                causalContext?.let {
                    JankHunterWorkerRuntime.endConcurrencyWorker(this, it, terminalOutcome)
                }
            } finally {
                previousContext?.let(JankHunterWorkerRuntime::restoreConcurrencyContext)
            }
        }
    }

    /** Implements the application work while [doWork] remains a balanced telemetry boundary. */
    protected abstract fun doJankHunterWork(): Result
}
