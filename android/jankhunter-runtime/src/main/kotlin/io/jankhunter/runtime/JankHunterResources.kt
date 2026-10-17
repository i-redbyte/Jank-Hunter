package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.locks.Lock

/** Categories of explicitly tracked leases. A lease never retains the application resource. */
enum class JankHunterResourceKind(internal val wire: Int) {
    AUTO_CLOSEABLE(1),
    LOCK(2),
    MONITOR(3),
    WAKE_LOCK(4),
}

/**
 * Optional resource contracts for detailed concurrency diagnostics.
 *
 * The ordinary automatic I/O events measure operations, without claiming resource ownership.
 * Use these boundaries where the application knows the exact acquisition/release API. They do
 * not replace or wrap the resource identity. A bound lease promises release by the current task;
 * transfer that promise explicitly if the resource leaves that task. With diagnostics disabled,
 * these methods preserve the application operations and produce no leases.
 */
object JankHunterResources {
    @JvmStatic
    @JvmOverloads
    fun beginAcquire(kind: JankHunterResourceKind, boundToCurrentScope: Boolean = false): JankHunterResourceLease? =
        beginAcquire(kind, boundToCurrentScope, 0L, null, 0, 0)

    /** Build plugin bridge; metadata names the application callsite, never a stack trace. */
    @JvmStatic
    fun beginAcquire(
        kind: JankHunterResourceKind, boundToCurrentScope: Boolean,
        siteId: Long, source: String?, line: Int, origin: Int,
    ): JankHunterResourceLease? = beginAcquireInternal(kind, boundToCurrentScope, siteId, source, line, origin,
        captureParent())

    @JvmStatic
    fun beginAcquireWithDefaults(
        kind: JankHunterResourceKind, boundToCurrentScope: Boolean, mask: Int,
        siteId: Long, source: String, line: Int, origin: Int,
    ): JankHunterResourceLease? {
        // The plugin drops the unused generated marker; only argument 2 has a default.
        return beginAcquire(kind, if (mask and 2 != 0) false else boundToCurrentScope, siteId, source, line, origin)
    }

    private fun beginAcquireInternal(
        kind: JankHunterResourceKind, boundToCurrentScope: Boolean,
        siteId: Long, source: String?, line: Int, origin: Int, parent: JankHunterCausalContext?,
    ): JankHunterResourceLease? = RuntimeHookGuard.value(null) {
        JankHunter.concurrencyTelemetry().beginResourceAcquire(kind.wire, siteId, source, line, origin,
            parent, boundToCurrentScope)
    }

    @JvmStatic
    fun acquired(lease: JankHunterResourceLease?) = RuntimeHookGuard.run {
        if (lease != null) JankHunter.concurrencyTelemetry().resourceAcquired(lease)
    }

    @JvmStatic
    fun acquireFailed(lease: JankHunterResourceLease?) = RuntimeHookGuard.run {
        if (lease != null) JankHunter.concurrencyTelemetry().resourceAcquireFailed(lease)
    }

    /** Call only after the actual close/unlock/release returned successfully. */
    @JvmStatic
    fun released(lease: JankHunterResourceLease?) = released(lease, 0L, null, 0, 0)

    @JvmStatic
    fun released(lease: JankHunterResourceLease?, siteId: Long, source: String?, line: Int, origin: Int) = RuntimeHookGuard.run {
        if (lease != null) JankHunter.concurrencyTelemetry().resourceReleased(lease, siteId, source, line, origin)
    }

    /** Keeps the existing promise when no active task can receive the lease. */
    @JvmStatic
    fun transferToCurrentScope(lease: JankHunterResourceLease?) = RuntimeHookGuard.run {
        if (lease != null) {
            val telemetry = JankHunter.concurrencyTelemetry()
            val scope = telemetry.captureCausalContext() ?: return@run
            telemetry.resourceTransferred(lease, scope)
        }
    }

    /** Removes the task-lifetime promise, for example when returning an open resource to a caller. */
    @JvmStatic
    fun transferOutsideScope(lease: JankHunterResourceLease?) = RuntimeHookGuard.run {
        if (lease != null) JankHunter.concurrencyTelemetry().resourceTransferred(lease, null)
    }

    /** Each reentrant acquisition has its own balanced lease and uses the original Lock. */
    fun <T> withLock(lock: Lock, block: () -> T): T = withLock(lock, block, 0L, null, 0, 0)

    @JvmStatic
    fun <T> withLock(lock: Lock, block: () -> T, siteId: Long, source: String?, line: Int, origin: Int): T {
        val parent = captureParent()
        val lease = beginAcquireInternal(JankHunterResourceKind.LOCK, true, siteId, source, line, origin, parent)
        val started = SystemClock.elapsedRealtimeNanos()
        try {
            lock.lock()
        } catch (failure: Throwable) {
            acquireFailed(lease)
            throw failure
        }
        acquired(lease)
        recordWait(RuntimeCausalOperationKind.LOCK_WAIT, started, parent, siteId, source, line, origin)
        try {
            return block()
        } finally {
            lock.unlock()
            released(lease, siteId, source, line, origin)
        }
    }

    /** The release event is emitted after MONITOREXIT, including exceptional exits. */
    fun <T> withMonitor(monitor: Any, block: () -> T): T = withMonitor(monitor, block, 0L, null, 0, 0)

    @JvmStatic
    fun <T> withMonitor(monitor: Any, block: () -> T, siteId: Long, source: String?, line: Int, origin: Int): T {
        val parent = captureParent()
        val lease = beginAcquireInternal(JankHunterResourceKind.MONITOR, true, siteId, source, line, origin, parent)
        val started = SystemClock.elapsedRealtimeNanos()
        var entered = false
        try {
            return synchronized(monitor) {
                entered = true
                acquired(lease)
                recordWait(RuntimeCausalOperationKind.MONITOR_WAIT, started, parent, siteId, source, line, origin)
                block()
            }
        } finally {
            if (entered) released(lease, siteId, source, line, origin) else acquireFailed(lease)
        }
    }

    /** Observes an existing resource passed to use; its acquisition and prior state remain unknown. */
    fun <R : AutoCloseable, T> use(resource: R, block: (R) -> T): T = use(resource, block, 0L, null, 0, 0)

    @JvmStatic
    fun <R : AutoCloseable, T> use(resource: R, block: (R) -> T, siteId: Long, source: String?, line: Int, origin: Int): T {
        val lease = RuntimeHookGuard.value<JankHunterResourceLease?>(null) {
            JankHunter.concurrencyTelemetry().beginResourceUse(siteId, source, line, origin, captureParent())
        }
        var primary: Throwable? = null
        try {
            return block(resource)
        } catch (failure: Throwable) {
            primary = failure
            throw failure
        } finally {
            if (primary == null) {
                resource.close()
                released(lease, siteId, source, line, origin)
            } else {
                try {
                    resource.close()
                    released(lease, siteId, source, line, origin)
                } catch (closeFailure: Throwable) {
                    if (closeFailure !== primary) primary.addSuppressed(closeFailure)
                }
            }
        }
    }

    private fun captureParent(): JankHunterCausalContext? = RuntimeHookGuard.value(null) {
        JankHunter.concurrencyTelemetry().captureCausalContext()
    }

    private fun recordWait(
        category: Int, started: Long, parent: JankHunterCausalContext?,
        siteId: Long, source: String?, line: Int, origin: Int,
    ) = RuntimeHookGuard.run {
        JankHunter.concurrencyTelemetry().recordCost(category,
            (SystemClock.elapsedRealtimeNanos() - started).coerceAtLeast(0L),
            -1L, -1L, siteId, source, line, origin, parent, android.os.Process.myTid().toLong())
    }
}
