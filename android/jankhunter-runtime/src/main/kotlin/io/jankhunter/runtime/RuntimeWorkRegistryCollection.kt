package io.jankhunter.runtime

import android.os.SystemClock
import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import io.jankhunter.runtime.internal.system.WorkRegistryCollector
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicLong

internal fun startWorkRegistryCollection(state: RuntimeState, config: JankHunterConfig, scheduler: RuntimeMaintenanceScheduler) {
    val writer = state.writer ?: return
    val interval = config.workerRegistryIntervalMs()
    val source = config.workerRegistrySource()
    if (source == null) {
        scheduler.execute {
            val now = SystemClock.elapsedRealtime()
            writer.workRegistry(1, now, now, interval,
                JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.NO_SOURCE))
        }
        return
    }
    val snapshots = AtomicLong()
    val collector = WorkRegistryCollector(
        source, Executor { task -> scheduler.execute { task.run() } }, SystemClock::elapsedRealtime,
        cleanup = state.workCleanup,
    ) { result, started, observed ->
        writer.workRegistry(snapshots.incrementAndGet(), started, observed, interval, result)
    }
    state.workRegistryCollector = collector
    collector.activate()
    state.workRegistryHandle = scheduler.schedule(delayMs = RuntimeLongSource { interval }) { collector.sampleOnce() }
}
