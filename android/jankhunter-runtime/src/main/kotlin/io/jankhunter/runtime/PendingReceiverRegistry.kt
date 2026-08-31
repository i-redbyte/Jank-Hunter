package io.jankhunter.runtime

import java.lang.ref.WeakReference

internal data class PendingReceiverSnapshot(
    val token: Long,
    val componentId: Long,
    val componentName: String,
    val action: String?,
    val instanceId: Long,
    val flags: Long,
)

/** Bounded weak-key registry; values contain approved metadata only and never retain Android objects. */
internal class PendingReceiverRegistry(
    capacity: Int = DEFAULT_CAPACITY,
    private val onEviction: () -> Unit = {},
    private val onResolutionMissAfterEviction: () -> Unit = {},
) {
    private val mask: Int
    private val references: Array<WeakReference<Any>?>
    private val snapshots: Array<PendingReceiverSnapshot?>
    private var evictionCursor = 0
    private var evictions = 0L

    init {
        require(capacity >= 2 && capacity and (capacity - 1) == 0)
        mask = capacity - 1
        references = arrayOfNulls(capacity)
        snapshots = arrayOfNulls(capacity)
    }

    @Synchronized
    fun register(pendingResult: Any, snapshot: PendingReceiverSnapshot): Boolean {
        val start = identityIndex(pendingResult)
        var reclaim = -1
        val probes = minOf(references.size, MAX_PROBES)
        for (offset in 0 until probes) {
            val index = (start + offset) and mask
            val current = references[index]?.get()
            if (current === pendingResult) {
                snapshots[index] = snapshot
                return true
            }
            if (current == null && reclaim < 0) reclaim = index
        }
        val target = if (reclaim >= 0) {
            reclaim
        } else {
            if (evictions != Long.MAX_VALUE) evictions++
            onEviction()
            (start + (evictionCursor++ and (probes - 1))) and mask
        }
        references[target] = WeakReference(pendingResult)
        snapshots[target] = snapshot
        return true
    }

    @Synchronized
    fun complete(pendingResult: Any?): PendingReceiverSnapshot? {
        if (pendingResult == null) return null
        val start = identityIndex(pendingResult)
        val probes = minOf(references.size, MAX_PROBES)
        for (offset in 0 until probes) {
            val index = (start + offset) and mask
            val current = references[index]?.get()
            if (current === pendingResult) {
                val snapshot = snapshots[index]
                references[index] = null
                snapshots[index] = null
                return snapshot
            }
            if (current == null) {
                references[index] = null
                snapshots[index] = null
            }
        }
        if (evictions != 0L) onResolutionMissAfterEviction()
        return null
    }

    fun cancel(pendingResult: Any?) {
        complete(pendingResult)
    }

    @Synchronized
    internal fun retainedEntryCount(): Int {
        var count = 0
        for (index in references.indices) {
            if (references[index]?.get() == null) {
                references[index] = null
                snapshots[index] = null
            } else {
                count++
            }
        }
        return count
    }

    private fun identityIndex(value: Any): Int {
        val hash = System.identityHashCode(value)
        return (hash xor (hash ushr 16)) and mask
    }

    private companion object {
        const val DEFAULT_CAPACITY = 1_024
        const val MAX_PROBES = 8
    }
}
