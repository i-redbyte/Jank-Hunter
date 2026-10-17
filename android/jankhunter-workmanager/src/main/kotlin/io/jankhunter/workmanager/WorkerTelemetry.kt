package io.jankhunter.workmanager

import android.os.Build
import androidx.work.ListenableWorker
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.runtime.JankHunterWorkerOutcome
import java.util.UUID

internal object WorkerTelemetry {
    private val successResultClass = ListenableWorker.Result.success().javaClass
    private val failureResultClass = ListenableWorker.Result.failure().javaClass
    private val retryResultClass = ListenableWorker.Result.retry().javaClass

    fun instanceId(id: UUID): Long = JankHunterWorkerRuntime.instanceId(id.mostSignificantBits, id.leastSignificantBits)

    fun outcome(result: ListenableWorker.Result): JankHunterWorkerOutcome = when (result.javaClass) {
        successResultClass -> JankHunterWorkerOutcome.SUCCESS
        failureResultClass -> JankHunterWorkerOutcome.FAILURE
        retryResultClass -> JankHunterWorkerOutcome.RETRY
        else -> JankHunterWorkerOutcome.UNKNOWN
    }

    fun finish(
        worker: ListenableWorker,
        token: Long,
        instanceId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
    ): JankHunterWorkerOutcome {
        val stopped = worker.isStopped
        val stopReasonKnown = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
        val stopReason = if (stopReasonKnown) worker.stopReason else 0
        val terminalOutcome = workerTerminalOutcome(stopped, outcome, stopReasonKnown, stopReason)
        if (token != 0L) JankHunterWorkerRuntime.finished(
            token = token,
            instanceId = instanceId,
            workerName = workerName,
            outcome = terminalOutcome,
            runAttempt = runAttempt,
            generation = generation,
            stopReason = stopReason,
            stopReasonKnown = stopReasonKnown,
        )
        return terminalOutcome
    }
}

/** A WorkManager stop can be caused by constraints, timeout or quota, not just cancellation. */
internal fun workerTerminalOutcome(
    stopped: Boolean,
    outcome: JankHunterWorkerOutcome,
    stopReasonKnown: Boolean,
    stopReason: Int,
): JankHunterWorkerOutcome = when {
    !stopped -> outcome
    stopReasonKnown && stopReason == androidx.work.WorkInfo.STOP_REASON_CANCELLED_BY_APP -> JankHunterWorkerOutcome.CANCELLED
    else -> JankHunterWorkerOutcome.STOPPED
}
