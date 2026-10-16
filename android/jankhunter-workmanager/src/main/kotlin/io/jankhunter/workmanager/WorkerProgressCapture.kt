package io.jankhunter.workmanager

import android.os.SystemClock
import androidx.work.Data
import io.jankhunter.runtime.JankHunterWorkerRuntime

/** Per-worker bound. Omitted calls are reported at the lifecycle boundary. */
internal class WorkerProgressLimiter {
    private var lastRecordedNanos = -1L
    private var recorded = 0
    private var omitted = 0
    private var finished = false

    @Synchronized fun accept(observedNanos: Long): Boolean {
        if (finished) return false
        if (recorded >= MAX_UPDATES ||
            (lastRecordedNanos >= 0 && observedNanos >= lastRecordedNanos &&
                observedNanos - lastRecordedNanos < MIN_INTERVAL_NANOS)) {
            if (omitted < Int.MAX_VALUE) omitted++
            return false
        }
        lastRecordedNanos = observedNanos
        recorded++
        return true
    }

    @Synchronized fun finish(): Int {
        finished = true
        return omitted
    }

    private companion object {
        const val MAX_UPDATES = 32
        const val MIN_INTERVAL_NANOS = 1_000_000_000L
    }
}

/** The token ties requests to the current collection epoch; no Data object survives the call. */
internal class WorkerProgressCapture {
    private val limiter = WorkerProgressLimiter()
    private var token = 0L
    private var instanceId = 0L
    private var runAttempt = 0
    private var generation = 0

    @Synchronized fun start(token: Long, instanceId: Long, runAttempt: Int, generation: Int) {
        this.token = token
        this.instanceId = instanceId
        this.runAttempt = runAttempt
        this.generation = generation
    }

    @Synchronized fun request(data: Data) {
        val active = token
        if (active == 0L) return
        val observed = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L)
        if (!limiter.accept(observed)) return
        val metadata = progressMetadata(data)
        JankHunterWorkerRuntime.progressRequested(active, instanceId, runAttempt, generation,
            observed, metadata.fields, metadata.omittedKeys, metadata.truncatedFields)
    }

    @Synchronized fun finish() {
        val active = token
        token = 0L
        val omitted = limiter.finish()
        if (active != 0L && omitted > 0) {
            JankHunterWorkerRuntime.progressOmitted(active, instanceId, runAttempt, generation,
                SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L), omitted)
        }
    }
}
