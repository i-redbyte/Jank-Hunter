package io.jankhunter.runtime.internal.io

/** Writer-owned primitive long-to-string map that avoids boxing stable ASM ids on every event. */
internal class StableSymbolRegistry(initialCapacity: Int = DEFAULT_CAPACITY) {
    private var keys = LongArray(tableCapacity(initialCapacity))
    private var values = arrayOfNulls<String>(keys.size)
    private var size = 0

    fun get(id: Long): String? {
        var index = hash(id) and (keys.size - 1)
        while (true) {
            val value = values[index] ?: return null
            if (keys[index] == id) return value
            index = (index + 1) and (keys.size - 1)
        }
    }

    fun put(id: Long, value: String) {
        if (size * 2 >= keys.size) grow()
        insert(id, value)
    }

    private fun insert(id: Long, value: String) {
        var index = hash(id) and (keys.size - 1)
        while (values[index] != null) {
            if (keys[index] == id) {
                values[index] = value
                return
            }
            index = (index + 1) and (keys.size - 1)
        }
        keys[index] = id
        values[index] = value
        size++
    }

    private fun grow() {
        val oldKeys = keys
        val oldValues = values
        keys = LongArray(oldKeys.size shl 1)
        values = arrayOfNulls(keys.size)
        size = 0
        for (index in oldValues.indices) {
            val value = oldValues[index] ?: continue
            insert(oldKeys[index], value)
        }
    }

    private fun hash(value: Long): Int {
        var mixed = value
        mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
        mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
        return (mixed xor (mixed ushr 33)).toInt()
    }

    private companion object {
        const val DEFAULT_CAPACITY = 256

        fun tableCapacity(requested: Int): Int {
            val minimum = requested.coerceAtLeast(1).coerceAtMost(MAX_INITIAL_CAPACITY) shl 1
            var capacity = 2
            while (capacity < minimum) capacity = capacity shl 1
            return capacity
        }

        const val MAX_INITIAL_CAPACITY = 1 shl 29
    }
}
