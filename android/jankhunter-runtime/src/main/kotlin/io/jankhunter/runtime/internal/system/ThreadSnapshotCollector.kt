package io.jankhunter.runtime.internal.system

import android.os.SystemClock
import io.jankhunter.runtime.RuntimeLongSource

internal class ThreadSnapshotCollector(
    private val intervalMs: Long,
    private val read: () -> ProcThreadSnapshot = ProcThreadReader()::read,
    private val beforeScan: () -> Unit = {},
    private val enrich: (ProcThreadSnapshot, Long, Long) -> ProcThreadSnapshot? = { snapshot, _, _ -> snapshot },
    private val clock: () -> Long = SystemClock::elapsedRealtime,
    private val clockNanos: () -> Long = SystemClock::elapsedRealtimeNanos,
    private val report: (ProcThreadSnapshot, Long, Long) -> Unit,
) {
    private val lock = Any()
    private var generation = 0L
    private var active = false
    private var handle: MaintenanceHandle? = null

    fun activate() = synchronized(lock) {
        generation++
        active = true
    }

    fun start(scheduler: RuntimeMaintenanceScheduler) = synchronized(lock) {
        check(!active)
        activate()
        handle = scheduler.schedule(delayMs = RuntimeLongSource { intervalMs.coerceAtLeast(500) }) { sampleOnce() }
    }

    fun stop() = synchronized(lock) {
        active = false
        generation++
        handle?.cancel()
        handle = null
    }

    /** Called only on the maintenance lane; stop may concurrently invalidate this scan. */
    fun sampleOnce() {
        val expected = synchronized(lock) { if (!active) return else generation }
        beforeScan()
        val started = clock()
        val startedNanos = clockNanos()
        val snapshot = enrich(read(), started, startedNanos) ?: return
        val duration = (clock() - started).coerceAtLeast(0)
        val admitted = synchronized(lock) { active && generation == expected }
        // An admitted report can finish on its captured epoch sink after stop. Never hold the
        // lifecycle monitor across serialization/admission: stop runs on application threads.
        if (admitted) report(snapshot, started, duration)
    }
}
