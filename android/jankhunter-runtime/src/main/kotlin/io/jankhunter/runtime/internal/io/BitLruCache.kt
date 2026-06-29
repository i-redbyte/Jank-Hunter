package io.jankhunter.runtime.internal.io

internal class BitLruCache<K : Any>(
    capacity: Int,
) {
    private val capacity = checkedCapacity(capacity)
    // rows[i] bit j is 1 when slot i was used more recently than slot j.
    private val rows = LongArray(capacity)
    private val keysBySlot = arrayOfNulls<Any>(capacity)
    private val slotsByKey = HashMap<K, Int>(capacity)
    private val capacityMask = capacityMask(capacity)
    private var liveMask = 0L

    val size: Int
        get() = slotsByKey.size

    fun admit(key: K): Admission<K> {
        slotsByKey[key]?.let { slot ->
            touchSlot(slot)
            return Admission(admitted = true, evicted = null)
        }
        if (capacity == 0) {
            return Admission(admitted = false, evicted = null)
        }

        val slot = firstFreeSlot().takeIf { it >= 0 } ?: leastRecentlyUsedSlot()
        if (slot < 0) {
            return Admission(admitted = false, evicted = null)
        }

        val evicted = keyAt(slot)
        if (evicted != null) {
            slotsByKey.remove(evicted)
            clearSlot(slot)
        }
        keysBySlot[slot] = key
        slotsByKey[key] = slot
        markLive(slot)
        touchSlot(slot)
        return Admission(admitted = true, evicted = evicted)
    }

    fun touch(key: K): Boolean {
        val slot = slotsByKey[key] ?: return false
        touchSlot(slot)
        return true
    }

    fun remove(key: K): Boolean {
        val slot = slotsByKey.remove(key) ?: return false
        keysBySlot[slot] = null
        clearSlot(slot)
        return true
    }

    fun contains(key: K): Boolean = slotsByKey.containsKey(key)

    fun clear() {
        rows.fill(0L)
        keysBySlot.fill(null)
        slotsByKey.clear()
        liveMask = 0L
    }

    private fun firstFreeSlot(): Int {
        val free = liveMask.inv() and capacityMask
        return if (free == 0L) -1 else java.lang.Long.numberOfTrailingZeros(free)
    }

    private fun leastRecentlyUsedSlot(): Int {
        var candidates = liveMask
        while (candidates != 0L) {
            val bit = candidates and -candidates
            val slot = java.lang.Long.numberOfTrailingZeros(bit)
            if ((rows[slot] and liveMask) == 0L) {
                return slot
            }
            candidates = candidates and (candidates - 1L)
        }
        return -1
    }

    private fun markLive(slot: Int) {
        liveMask = liveMask or bit(slot)
    }

    private fun clearSlot(slot: Int) {
        val clearMask = bit(slot).inv()
        liveMask = liveMask and clearMask
        rows[slot] = 0L
        for (index in rows.indices) {
            rows[index] = rows[index] and clearMask
        }
    }

    private fun touchSlot(slot: Int) {
        val slotBit = bit(slot)
        rows[slot] = liveMask and slotBit.inv()
        val clearMask = slotBit.inv()
        for (index in rows.indices) {
            if (index != slot) {
                rows[index] = rows[index] and clearMask
            }
        }
    }

    @Suppress("UNCHECKED_CAST")
    private fun keyAt(slot: Int): K? = keysBySlot[slot] as K?

    private fun bit(slot: Int): Long = 1L shl slot

    data class Admission<K : Any>(
        val admitted: Boolean,
        val evicted: K?,
    )

    companion object {
        const val MAX_CAPACITY = 64

        private fun checkedCapacity(capacity: Int): Int {
            require(capacity in 0..MAX_CAPACITY) {
                "BitLruCache capacity must be between 0 and $MAX_CAPACITY"
            }
            return capacity
        }

        private fun capacityMask(capacity: Int): Long {
            return when (capacity) {
                0 -> 0L
                MAX_CAPACITY -> -1L
                else -> (1L shl capacity) - 1L
            }
        }
    }
}
