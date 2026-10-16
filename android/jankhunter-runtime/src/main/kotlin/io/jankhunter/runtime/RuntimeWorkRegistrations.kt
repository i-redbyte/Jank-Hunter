package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicReference

/** Epoch-owned, bounded retention. External futures retain only a cleared lease after close. */
internal class RuntimeWorkRegistrations(
    private val clock: RuntimeLongSource,
    private val cleanup: RuntimeWorkCleanup,
    private val capacity: Int = 64,
    private val timeoutMs: Long = 60_000,
    private val lost: (Int) -> Unit = {},
) {
    private val entries = LinkedHashSet<RuntimeWorkRegistration>()
    private var closed = false

    init { require(capacity > 0 && timeoutMs > 0) }

    fun reserve(cancel: Runnable, publish: (Long, Boolean) -> Unit): RuntimeWorkRegistration? {
        var full = false
        val lease = synchronized(this) {
            if (closed) null else if (entries.size >= capacity) {
                full = true
                null
            } else {
                RuntimeWorkRegistration(RegistrationState(this, clock.getAsLong(), cancel, publish)).also(entries::add)
            }
        }
        if (full) lost(CAPACITY)
        return lease
    }

    fun expire() {
        val snapshot = synchronized(this) { entries.toList() }
        snapshot.forEach { it.isActive() }
    }

    fun close() {
        val snapshot = synchronized(this) {
            if (closed) return
            closed = true
            entries.toList().also { entries.clear() }
        }
        snapshot.forEach { it.discard(CLOSED) }
    }

    internal fun expired(started: Long): Boolean {
        val now = clock.getAsLong()
        return now < started || now - started >= timeoutMs
    }

    internal fun removed(lease: RuntimeWorkRegistration, reason: Int?) {
        synchronized(this) { entries.remove(lease) }
        if (reason != null) lost(reason)
    }

    // Capture only the detached callback, never RegistrationState or the old epoch/writer.
    internal fun cancel(callback: Runnable) = cleanup.execute(callback::run)

    companion object {
        const val CAPACITY = 1
        const val TIMEOUT = 2
        const val CLOSED = 3
    }
}

internal class RegistrationState(
    val owner: RuntimeWorkRegistrations,
    val started: Long,
    val cancel: Runnable,
    val publish: (Long, Boolean) -> Unit,
)

internal class RuntimeWorkRegistration(state: RegistrationState) : JankHunterWorkRegistration {
    private val state = AtomicReference<RegistrationState?>(state)

    override fun isActive(): Boolean {
        val current = state.get() ?: return false
        if (current.owner.expired(current.started)) {
            discard(RuntimeWorkRegistrations.TIMEOUT)
            return false
        }
        return state.get() === current
    }

    override fun observed(instanceId: Long, periodic: Boolean) {
        if (!isActive()) return
        // Publication already in progress may finish in its original writer; it
        // never looks up a replacement epoch or transfers observations to it.
        state.get()?.publish?.invoke(instanceId, periodic)
    }

    override fun close() = release(null)

    fun discard(reason: Int) = release(reason)

    private fun release(reason: Int?) {
        val current = state.getAndSet(null) ?: return
        current.owner.removed(this, reason)
        if (reason != null) current.owner.cancel(current.cancel)
    }
}
