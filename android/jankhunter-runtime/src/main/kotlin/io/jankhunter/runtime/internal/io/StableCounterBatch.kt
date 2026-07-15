package io.jankhunter.runtime.internal.io

/** Bounded primitive hand-off for counters whose metric name is a stable symbol reference. */
internal class StableCounterBatch(capacity: Int) {
    private val ids = LongArray(capacity)
    private val names = arrayOfNulls<String>(capacity)
    private val values = LongArray(capacity)

    var size: Int = 0
        private set

    fun add(id: Long, value: Long) = add(id, null, value)

    fun add(id: Long, name: String?, value: Long) {
        check(size < ids.size) { "Stable counter batch capacity exceeded" }
        ids[size] = id
        names[size] = name
        values[size] = value
        size++
    }

    fun id(index: Int): Long = ids[index]

    fun name(index: Int): String? = names[index]

    fun value(index: Int): Long = values[index]
}
