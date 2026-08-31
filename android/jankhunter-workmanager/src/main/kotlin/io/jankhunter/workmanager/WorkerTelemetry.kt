package io.jankhunter.workmanager

import android.os.Build
import androidx.work.ListenableWorker
import io.jankhunter.runtime.JankHunterWorkerRuntime
import io.jankhunter.runtime.JankHunterWorkerOutcome
import java.util.UUID

internal object WorkerTelemetry {
    fun instanceId(id: UUID): Long = JankHunterWorkerRuntime.instanceId(id.mostSignificantBits, id.leastSignificantBits)

    fun outcome(result: ListenableWorker.Result): JankHunterWorkerOutcome = JankHunterWorkerRuntime.classify(result)

    fun finish(
        worker: ListenableWorker,
        token: Long,
        instanceId: Long,
        workerName: String,
        outcome: JankHunterWorkerOutcome,
        runAttempt: Int,
        generation: Int,
    ) {
        val stopped = worker.isStopped
        val terminalOutcome = if (stopped) JankHunterWorkerOutcome.CANCELLED else outcome
        val stopReasonKnown = Build.VERSION.SDK_INT >= Build.VERSION_CODES.S
        JankHunterWorkerRuntime.finished(
            token = token,
            instanceId = instanceId,
            workerName = workerName,
            outcome = terminalOutcome,
            runAttempt = runAttempt,
            generation = generation,
            stopReason = if (stopReasonKnown) worker.stopReason else 0,
            stopReasonKnown = stopReasonKnown,
        )
    }
}
