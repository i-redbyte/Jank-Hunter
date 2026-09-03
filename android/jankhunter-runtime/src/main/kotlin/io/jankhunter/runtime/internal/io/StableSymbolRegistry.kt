package io.jankhunter.runtime.internal.io

/** Writer-owned primitive long-to-string map that avoids boxing stable ASM ids on every event. */
internal class StableSymbolRegistry(initialCapacity: Int = DEFAULT_CAPACITY) {
    private var keys = LongArray(tableCapacity(initialCapacity))
    private var values = arrayOfNulls<String>(keys.size)
    private var aliases = LongArray(keys.size)
    private var size = 0
    private var nextAlias = 1L

    fun get(id: Long): String? {
        var index = hash(id) and (keys.size - 1)
        while (true) {
            val value = values[index] ?: return null
            if (keys[index] == id) return value
            index = (index + 1) and (keys.size - 1)
        }
    }

    fun alias(id: Long): Long {
        var index = hash(id) and (keys.size - 1)
        while (true) {
            if (values[index] == null) return 0L
            if (keys[index] == id) return aliases[index]
            index = (index + 1) and (keys.size - 1)
        }
    }

    fun put(id: Long, value: String): Long {
        if (size * 2 >= keys.size) grow()
        return insert(id, value, 0L)
    }

    private fun insert(id: Long, value: String, retainedAlias: Long): Long {
        var index = hash(id) and (keys.size - 1)
        while (values[index] != null) {
            if (keys[index] == id) {
                values[index] = value
                return aliases[index]
            }
            index = (index + 1) and (keys.size - 1)
        }
        val alias = retainedAlias.takeIf { it > 0L } ?: nextAlias++
        keys[index] = id
        values[index] = value
        aliases[index] = alias
        size++
        return alias
    }

    private fun grow() {
        val oldKeys = keys
        val oldValues = values
        val oldAliases = aliases
        keys = LongArray(oldKeys.size shl 1)
        values = arrayOfNulls(keys.size)
        aliases = LongArray(keys.size)
        size = 0
        for (index in oldValues.indices) {
            val value = oldValues[index] ?: continue
            insert(oldKeys[index], value, oldAliases[index])
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
