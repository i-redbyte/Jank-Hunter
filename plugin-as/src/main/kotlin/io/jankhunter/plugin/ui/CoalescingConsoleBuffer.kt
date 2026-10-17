package io.jankhunter.plugin.ui

/** Bounded tail and one pending UI delivery; a stalled EDT never causes per-chunk tail shifting. */
internal class CoalescingConsoleBuffer(private val capacity: Int) : AutoCloseable {
    private var data: CharArray? = null
    private var start = 0
    private var size = 0
    private var scheduled = false
    private var closed = false

    init { require(capacity > 0) }

    @Synchronized fun offer(chunk: String): Boolean {
        if (closed || chunk.isEmpty()) return false
        val buffer = data ?: CharArray(capacity).also { data = it }
        if (chunk.length >= capacity) {
            chunk.toCharArray(buffer, 0, chunk.length - capacity, chunk.length)
            start = 0
            size = capacity
        } else {
            val end = (start + size) % capacity
            val first = minOf(chunk.length, capacity - end)
            chunk.toCharArray(buffer, end, 0, first)
            if (first < chunk.length) chunk.toCharArray(buffer, 0, first, chunk.length)
            val overflow = (size + chunk.length - capacity).coerceAtLeast(0)
            start = (start + overflow) % capacity
            size = minOf(capacity, size + chunk.length)
        }
        if (scheduled) return false
        scheduled = true
        return true
    }

    @Synchronized fun drain(): String {
        scheduled = false
        val buffer = data ?: return ""
        val first = minOf(size, capacity - start)
        return buildString(size) {
            append(buffer, start, first)
            if (first < size) append(buffer, 0, size - first)
        }.also { size = 0; start = 0 }
    }

    @Synchronized override fun close() {
        closed = true
        data = null
        size = 0
    }
}
