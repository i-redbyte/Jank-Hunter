package io.jankhunter.workmanager

import com.google.common.util.concurrent.ListenableFuture
import java.util.concurrent.Executor

/** Captures a confirmed foreground transition and the end of this attempt's observation. */
internal class WorkerForegroundCapture(record: (Boolean) -> Unit) {
    private var record: ((Boolean) -> Unit)? = record
    private var active = true
    private var entered = false

    fun observe(future: ListenableFuture<Void>) {
        future.addListener({
            if (!future.isCancelled) {
                try {
                    future.get()
                    entered()
                } catch (_: Exception) { /* A failed transition is not evidence of foreground. */ }
            }
        }, Executor { action -> action.run() })
    }

    @Synchronized fun entered() {
        if (!active || entered) return
        entered = true
        try { record?.invoke(true) } catch (_: Exception) { /* Telemetry must not change WorkManager. */ }
    }

    @Synchronized fun finish() {
        if (!active) return
        active = false
        val recorder = record
        record = null
        if (entered) {
            try { recorder?.invoke(false) } catch (_: Exception) { /* Telemetry must not change WorkManager. */ }
        }
    }
}
