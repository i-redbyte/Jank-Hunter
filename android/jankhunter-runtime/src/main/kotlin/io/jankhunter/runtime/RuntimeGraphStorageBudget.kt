package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock

/**
 * Conservative retained producer storage, including stack/page arrays and their owners. Each
 * session has one quota; RuntimeCallGraph admits at most two sessions. Consumer tables, writer
 * queues and JVM-owned ThreadLocal maps have their own lifetimes and are not an RSS quota here.
 * No application callbacks or array allocations run while the small recycle list is locked.
 */
internal class RuntimeGraphStorageBudget(val limitBytes: Long = DEFAULT_LIMIT_BYTES) {
    private val used = AtomicLong()
    private val peak = AtomicLong()
    private val pressure = AtomicBoolean()
    private val recycledLock = ReentrantLock()
    private var recycled: RuntimeGraphAggregatePage? = null
    private var recycledCount = 0
    @Volatile private var closed = false

    init { require(limitBytes >= INITIAL_PRODUCER_BYTES + PAGE_BYTES) }

    fun tryReserve(bytes: Long): Boolean {
        require(bytes > 0L)
        repeat(RESERVATION_ATTEMPTS) {
            if (closed) return false
            val current = used.get()
            if (bytes > limitBytes - current) {
                if (discardRecycledPage()) return@repeat
                pressure.set(true)
                return false
            }
            if (used.compareAndSet(current, current + bytes)) {
                if (closed) {
                    release(bytes)
                    return false
                }
                updatePeak(current + bytes)
                return true
            }
        }
        // A contended admission also yields promptly; the caller owns retry/loss accounting.
        pressure.set(true)
        return false
    }

    fun release(bytes: Long) {
        require(bytes > 0L)
        check(used.addAndGet(-bytes) >= 0L) { "Runtime graph storage released twice" }
    }

    fun acquirePage(): RuntimeGraphAggregatePage? {
        if (recycledLock.tryLock()) {
            try {
                if (closed) return null
                removeRecycledPage()?.let { return it }
            } finally { unlockCache() }
        }
        if (!tryReserve(PAGE_BYTES)) return null
        return try { RuntimeGraphAggregatePage() } catch (failure: Throwable) {
            release(PAGE_BYTES)
            throw failure
        }
    }

    /** The caller has removed its page reference and cleared every application label. */
    fun recyclePage(page: RuntimeGraphAggregatePage) {
        check(page.size == 0 && page.recycledNext == null)
        if (recycledLock.tryLock()) {
            try {
                if (!closed && recycledCount < MAX_RECYCLED_PAGES) {
                    page.recycledNext = recycled
                    recycled = page
                    recycledCount++
                    return
                }
            } finally { unlockCache() }
        }
        release(PAGE_BYTES)
    }

    fun consumePressure(): Boolean = pressure.getAndSet(false)

    fun close() {
        closed = true
        clearClosedCache()
    }

    fun usedBytes(): Long = used.get()
    fun peakBytes(): Long = peak.get()

    private fun discardRecycledPage(): Boolean {
        if (!recycledLock.tryLock()) return false
        try { removeRecycledPage() ?: return false } finally { unlockCache() }
        release(PAGE_BYTES)
        return true
    }

    private fun removeRecycledPage(): RuntimeGraphAggregatePage? {
        val page = recycled ?: return null
        recycled = page.recycledNext
        page.recycledNext = null
        recycledCount--
        return page
    }

    private fun unlockCache() {
        recycledLock.unlock()
        // Close can lose tryLock to this owner. Check AFTER unlocking so that a concurrent close
        // either acquires the cache itself or leaves its cleanup to an owner that sees closed.
        if (closed) clearClosedCache()
    }

    private fun clearClosedCache() {
        if (!recycledLock.tryLock()) return
        try {
            while (removeRecycledPage() != null) release(PAGE_BYTES)
        } finally { recycledLock.unlock() }
    }

    private fun updatePeak(value: Long) {
        var previous = peak.get()
        while (value > previous) {
            if (peak.compareAndSet(previous, value)) return
            previous = peak.get()
        }
    }

    companion object {
        const val DEFAULT_LIMIT_BYTES = 8L * 1024L * 1024L
        // 8-byte references, 32-byte array headers; metadata includes registry/weak-owner slots.
        const val METADATA_BYTES = 2_048L
        const val INITIAL_STACK_DEPTH = 64
        const val PAGE_BYTES = 11_264L
        const val INITIAL_PRODUCER_BYTES = METADATA_BYTES + INITIAL_STACK_DEPTH * 40L + 5L * 32L
        private const val MAX_RECYCLED_PAGES = 16
        private const val RESERVATION_ATTEMPTS = 32
        fun stackBytes(capacity: Int): Long = capacity * 40L + 5L * 32L
    }
}
