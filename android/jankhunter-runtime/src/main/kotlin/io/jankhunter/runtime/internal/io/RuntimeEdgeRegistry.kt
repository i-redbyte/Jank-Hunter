package io.jankhunter.runtime.internal.io

/**
 * Segment-local exact registry for runtime graph edges.
 *
 * Entry arrays are append-only, while [slots] contains entry index + 1. This keeps lookup keys in
 * four dense primitive arrays and makes rehashing independent from wire IDs. The zero token is the
 * bounded inline fallback; positive even tokens reference an entry and odd tokens define one.
 */
internal class RuntimeEdgeRegistry(
    private val maxEntries: Int = DEFAULT_MAX_ENTRIES,
    initialCapacity: Int = DEFAULT_INITIAL_CAPACITY,
) {
    private var screens = LongArray(entryCapacity(initialCapacity, maxEntries))
    private var callers = LongArray(screens.size)
    private var operations = LongArray(screens.size)
    private var callees = LongArray(screens.size)
    private var slots = IntArray(tableCapacity(screens.size))
    private var size = 0

    init {
        require(maxEntries > 0) { "Runtime edge registry capacity must be positive" }
    }

    /** Returns an inline, reference, or definition token without allocating. */
    fun resolve(screen: Long, caller: Long, operation: Long, callee: Long): Long {
        require(screen >= 0L && caller > 0L && operation >= 0L && callee > 0L)
        var slot = findSlot(screen, caller, operation, callee)
        val encodedIndex = slots[slot]
        if (encodedIndex != 0) return encodedIndex.toLong() shl 1
        if (size == maxEntries) return INLINE_TOKEN

        ensureEntryCapacity()
        if ((size + 1) * MAX_LOAD_DENOMINATOR > slots.size) {
            growTable()
            slot = findSlot(screen, caller, operation, callee)
        }
        val index = size++
        screens[index] = screen
        callers[index] = caller
        operations[index] = operation
        callees[index] = callee
        val wireId = index + 1
        slots[slot] = wireId
        return (wireId.toLong() shl 1) or DEFINITION_BIT
    }

    internal fun entryCount(): Int = size

    private fun findSlot(screen: Long, caller: Long, operation: Long, callee: Long): Int {
        var slot = edgeHash(screen, caller, operation, callee) and (slots.size - 1)
        while (true) {
            val encodedIndex = slots[slot]
            if (encodedIndex == 0) return slot
            val index = encodedIndex - 1
            if (
                screens[index] == screen &&
                callers[index] == caller &&
                operations[index] == operation &&
                callees[index] == callee
            ) {
                return slot
            }
            slot = (slot + 1) and (slots.size - 1)
        }
    }

    private fun ensureEntryCapacity() {
        if (size < screens.size) return
        val next = minOf(maxEntries, screens.size shl 1)
        screens = screens.copyOf(next)
        callers = callers.copyOf(next)
        operations = operations.copyOf(next)
        callees = callees.copyOf(next)
    }

    private fun growTable() {
        if (slots.size >= maxEntries shl 1) return
        slots = IntArray(minOf(maxEntries shl 1, slots.size shl 1))
        for (index in 0 until size) {
            val slot = findSlot(screens[index], callers[index], operations[index], callees[index])
            slots[slot] = index + 1
        }
    }

    private companion object {
        const val DEFAULT_MAX_ENTRIES = 65_536
        const val DEFAULT_INITIAL_CAPACITY = 256
        const val MAX_LOAD_DENOMINATOR = 2
        const val INLINE_TOKEN = 0L
        const val DEFINITION_BIT = 1L

        fun entryCapacity(requested: Int, maximum: Int): Int {
            require(requested > 0) { "Runtime edge registry initial capacity must be positive" }
            return requested.coerceAtMost(maximum)
        }

        fun tableCapacity(entryCapacity: Int): Int {
            var result = 2
            val target = entryCapacity shl 1
            while (result < target) result = result shl 1
            return result
        }

        fun edgeHash(screen: Long, caller: Long, operation: Long, callee: Long): Int {
            var mixed = mix(screen xor java.lang.Long.rotateLeft(caller, 17))
            mixed = mix(mixed xor java.lang.Long.rotateLeft(operation, 31))
            mixed = mix(mixed xor java.lang.Long.rotateLeft(callee, 47))
            return mixed.toInt()
        }

        fun mix(value: Long): Long {
            var mixed = value
            mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
            mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
            return mixed xor (mixed ushr 33)
        }
    }
}
