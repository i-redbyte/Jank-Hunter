package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd

/** Primitive open-addressing table for consumer-side log-spam aggregation. */
internal class LogSpamAccumulator(maxEntries: Int) {
    private val entryLimit = maxEntries.coerceAtLeast(1)
    private var screens = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var owners = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var sources = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var levels = IntArray(INITIAL_CAPACITY)
    private var operationIds = LongArray(INITIAL_CAPACITY)
    private var counts = LongArray(INITIAL_CAPACITY)

    var size = 0
        private set

    private var logicalEvents = 0L

    fun add(screen: String?, owner: String?, source: String?, level: Int, operationId: Long): Boolean {
        var index = find(screen, owner, source, level, operationId)
        if (index >= 0) {
            counts[index] = saturatingAdd(counts[index], 1L)
            logicalEvents = saturatingAdd(logicalEvents, 1L)
            return true
        }
        if (size >= entryLimit) return false
        ensureInsertCapacity()
        index = find(screen, owner, source, level, operationId).inv()
        screens[index] = screen
        owners[index] = owner
        sources[index] = source
        levels[index] = level
        operationIds[index] = operationId
        counts[index] = 1L
        size++
        logicalEvents = saturatingAdd(logicalEvents, 1L)
        return true
    }

    fun logicalEventCount(): Long = logicalEvents

    fun drain(consumer: LogSpamConsumer) {
        for (index in counts.indices) {
            val count = counts[index]
            if (count == 0L) continue
            consumer.accept(screens[index], owners[index], sources[index], levels[index], operationIds[index], count)
            screens[index] = null
            owners[index] = null
            sources[index] = null
            levels[index] = 0
            operationIds[index] = 0L
            counts[index] = 0L
        }
        size = 0
        logicalEvents = 0L
    }

    private fun find(screen: String?, owner: String?, source: String?, level: Int, operationId: Long): Int {
        val mask = counts.lastIndex
        var index = hash(screen, owner, source, level, operationId) and mask
        while (counts[index] != 0L) {
            if (levels[index] == level && operationIds[index] == operationId &&
                screens[index] == screen && owners[index] == owner && sources[index] == source
            ) {
                return index
            }
            index = (index + 1) and mask
        }
        return index.inv()
    }

    private fun ensureInsertCapacity() {
        if ((size + 1) * 2 <= counts.size) return
        val oldScreens = screens
        val oldOwners = owners
        val oldSources = sources
        val oldLevels = levels
        val oldOperationIds = operationIds
        val oldCounts = counts
        val capacity = counts.size shl 1
        screens = arrayOfNulls(capacity)
        owners = arrayOfNulls(capacity)
        sources = arrayOfNulls(capacity)
        levels = IntArray(capacity)
        operationIds = LongArray(capacity)
        counts = LongArray(capacity)
        for (oldIndex in oldCounts.indices) {
            val count = oldCounts[oldIndex]
            if (count == 0L) continue
            val index = find(
                oldScreens[oldIndex], oldOwners[oldIndex], oldSources[oldIndex],
                oldLevels[oldIndex], oldOperationIds[oldIndex],
            ).inv()
            screens[index] = oldScreens[oldIndex]
            owners[index] = oldOwners[oldIndex]
            sources[index] = oldSources[oldIndex]
            levels[index] = oldLevels[oldIndex]
            operationIds[index] = oldOperationIds[oldIndex]
            counts[index] = count
        }
    }

    private fun hash(screen: String?, owner: String?, source: String?, level: Int, operationId: Long): Int {
        var value = screen?.hashCode() ?: 0
        value = 31 * value + (owner?.hashCode() ?: 0)
        value = 31 * value + (source?.hashCode() ?: 0)
        value = 31 * value + level
        value = 31 * value + (operationId xor (operationId ushr 32)).toInt()
        return value xor (value ushr 16)
    }

    private companion object {
        const val INITIAL_CAPACITY = 16
    }
}

internal fun interface LogSpamConsumer {
    fun accept(screen: String?, owner: String?, source: String?, level: Int, operationId: Long, count: Long)
}
