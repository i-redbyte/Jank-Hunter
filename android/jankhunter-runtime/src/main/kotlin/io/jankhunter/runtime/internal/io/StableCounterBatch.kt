package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.saturatingAdd

internal class StableCounterBatch(
    capacity: Int,
    private val pool: StableCounterBatchPool? = null,
) {
    private val ids = LongArray(capacity)
    private val names = arrayOfNulls<String>(capacity)
    private val values = LongArray(capacity)

    var size: Int = 0
        private set
    private var leased = pool != null

    fun add(id: Long, name: String, value: Long) {
        check(size < ids.size) { "Stable counter batch capacity exceeded" }
        ids[size] = id
        names[size] = name
        values[size] = value
        size++
    }

    fun id(index: Int): Long = ids[index]

    fun name(index: Int): String = checkNotNull(names[index])

    fun value(index: Int): Long = values[index]

    fun logicalEventCount(): Long {
        var result = 0L
        for (index in 0 until size) {
            result = saturatingAdd(result, values[index])
        }
        return result
    }

    internal fun recycle() {
        val target = pool ?: return
        if (!leased) return
        for (index in 0 until size) names[index] = null
        size = 0
        leased = false
        target.release(this)
    }

    internal fun prepareForReuse() {
        check(pool != null && !leased) { "Stable counter batch is already leased" }
        leased = true
    }
}
