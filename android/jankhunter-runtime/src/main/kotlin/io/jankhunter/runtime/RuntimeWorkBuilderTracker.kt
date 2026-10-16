package io.jankhunter.runtime

import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock

/** Epoch-owned, bounded evidence from public WorkRequest.Builder calls. */
internal class RuntimeWorkBuilderTracker(private val capacity: Int = 128) {
    data class Hints(val backoffPolicy: Int?, val backoffDelayMS: Long?, val quotaPolicy: Int?, val periodMS: Long? = null, val flexMS: Long? = null, val initialDelayMS: Long? = null)
    private data class Id(val most: Long, val least: Long)
    private class Node(builder: Any) {
        val key = WeakReference(builder)
        var backoffPolicy: Int? = null
        var backoffDelayMS: Long? = null
        var quotaPolicy: Int? = null
        var periodMS: Long? = null
        var flexMS: Long? = null
        var initialDelayMS: Long? = null
        fun hints() = Hints(backoffPolicy, backoffDelayMS, quotaPolicy, periodMS, flexMS, initialDelayMS)
    }

    private val lock = ReentrantLock()
    private val invalidation = AtomicLong()
    private var appliedInvalidation = 0L
    private val builders = ArrayList<Node>()
    private val built = LinkedHashMap<Id, Hints?>()

    init { require(capacity in 1..4096) }

    fun periodic(builder: Any, periodMS: Long, flexMS: Long?): Boolean = guarded(false) {
        if (periodMS < 0 || flexMS != null && flexMS < 0) return@guarded false
        val node = node(builder) ?: return@guarded false
        node.periodMS = periodMS
        node.flexMS = flexMS
        return@guarded true
    }

    fun initialDelay(builder: Any, delayMS: Long): Boolean = guarded(false) {
        if (delayMS < 0) {
            findNode(builder)?.initialDelayMS = null
            return@guarded false
        }
        val node = node(builder) ?: return@guarded false
        node.initialDelayMS = delayMS
        return@guarded true
    }

    fun backoff(builder: Any, policy: Int, delayMS: Long): Boolean = guarded(false) {
        if (policy !in 1..2 || delayMS < 0) {
            findNode(builder)?.apply { backoffPolicy = null; backoffDelayMS = null }
            return@guarded false
        }
        val node = node(builder) ?: return@guarded false
        node.backoffPolicy = policy
        node.backoffDelayMS = delayMS
        return@guarded true
    }

    fun expedited(builder: Any, quotaPolicy: Int): Boolean = guarded(false) {
        if (quotaPolicy !in 1..2) return@guarded false
        val node = node(builder) ?: return@guarded false
        node.quotaPolicy = quotaPolicy
        return@guarded true
    }

    fun built(builder: Any, most: Long, least: Long): Boolean = guarded(false) {
        val hints = findNode(builder)?.hints()
        val id = Id(most, least)
        if (built.containsKey(id)) {
            val previous = built[id]
            if (previous != hints) {
                built[id] = null
                return@guarded false
            }
            return@guarded true
        }
        if (built.size >= capacity) {
            if (hints == null) return@guarded true
            val oldest = built.entries.iterator()
            oldest.next()
            oldest.remove()
            built[id] = hints
            return@guarded false
        }
        built[id] = hints
        return@guarded true
    }

    fun find(most: Long, least: Long): Hints? = guarded(null) { built[Id(most, least)] }

    private inline fun <T> guarded(fallback: T, block: () -> T): T {
        if (!lock.tryLock()) {
            invalidation.incrementAndGet()
            return fallback
        }
        try {
            val version = invalidation.get()
            if (version != appliedInvalidation) {
                builders.clear()
                built.clear()
                appliedInvalidation = version
            }
            val result = block()
            return if (invalidation.get() == version) result else fallback
        } finally { lock.unlock() }
    }

    private fun node(builder: Any): Node? {
        findNode(builder)?.let { return it }
        if (builders.size >= capacity) return null
        return Node(builder).also(builders::add)
    }

    private fun findNode(builder: Any): Node? {
        var index = 0
        while (index < builders.size) {
            val candidate = builders[index]
            val current = candidate.key.get()
            if (current == null) builders.removeAt(index)
            else if (current === builder) return candidate
            else index++
        }
        return null
    }
}
