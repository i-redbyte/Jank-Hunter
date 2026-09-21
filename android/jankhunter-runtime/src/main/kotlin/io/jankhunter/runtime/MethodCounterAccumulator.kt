package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd

/** Primitive open-addressing table for consumer-side method counter aggregation. */
internal class MethodCounterAccumulator(maxEntries: Int) {
    private val entryLimit = maxEntries.coerceAtLeast(1)
    private var ids = LongArray(INITIAL_CAPACITY)
    private var names = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var counts = LongArray(INITIAL_CAPACITY)

    var size = 0
        private set

    private var logicalEvents = 0L

    fun add(id: Long, name: String): Boolean {
        var index = find(id)
        if (index >= 0) {
            counts[index] = saturatingAdd(counts[index], 1L)
            logicalEvents = saturatingAdd(logicalEvents, 1L)
            return true
        }
        if (size >= entryLimit) return false
        ensureInsertCapacity()
        index = find(id).inv()
        ids[index] = id
        names[index] = name
        counts[index] = 1L
        size++
        logicalEvents = saturatingAdd(logicalEvents, 1L)
        return true
    }

    fun logicalEventCount(): Long = logicalEvents

    fun drain(consumer: MethodCounterConsumer) {
        for (index in counts.indices) {
            val count = counts[index]
            if (count == 0L) continue
            consumer.accept(ids[index], checkNotNull(names[index]), count)
            ids[index] = 0L
            names[index] = null
            counts[index] = 0L
        }
        size = 0
        logicalEvents = 0L
    }

    private fun find(id: Long): Int {
        val mask = counts.lastIndex
        var index = hash(id) and mask
        while (counts[index] != 0L) {
            if (ids[index] == id) return index
            index = (index + 1) and mask
        }
        return index.inv()
    }

    private fun ensureInsertCapacity() {
        if ((size + 1) * 2 <= counts.size) return
        val oldIds = ids
        val oldNames = names
        val oldCounts = counts
        val capacity = counts.size shl 1
        ids = LongArray(capacity)
        names = arrayOfNulls(capacity)
        counts = LongArray(capacity)
        for (oldIndex in oldCounts.indices) {
            val count = oldCounts[oldIndex]
            if (count == 0L) continue
            val index = find(oldIds[oldIndex]).inv()
            ids[index] = oldIds[oldIndex]
            names[index] = oldNames[oldIndex]
            counts[index] = count
        }
    }

    private fun hash(id: Long): Int {
        var value = id
        value = (value xor (value ushr 33)) * -49064778989728563L
        value = (value xor (value ushr 33)) * -4265267296055464877L
        return (value xor (value ushr 33)).toInt()
    }

    private companion object {
        const val INITIAL_CAPACITY = 16
    }
}

internal fun interface MethodCounterConsumer {
    fun accept(id: Long, name: String, count: Long)
}
