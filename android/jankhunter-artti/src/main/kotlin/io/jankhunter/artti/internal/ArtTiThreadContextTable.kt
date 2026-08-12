package io.jankhunter.artti.internal

/** Single-drain-thread, fixed-capacity mapping used for contention-trigger attribution. */
internal class ArtTiThreadContextTable(capacity: Int) {
    private val keys = LongArray(capacity.coerceAtLeast(1))
    private val values = LongArray(keys.size)

    fun put(threadToken: Long, contextToken: Long): Boolean {
        if (threadToken <= 0L || threadToken == TOMBSTONE || contextToken == 0L) return false
        val initial = index(threadToken)
        var tombstone = -1
        repeat(minOf(keys.size, MAX_PROBES)) { probe ->
            val slot = (initial + probe) % keys.size
            when (keys[slot]) {
                threadToken -> {
                    values[slot] = contextToken
                    return true
                }
                TOMBSTONE -> if (tombstone < 0) tombstone = slot
                EMPTY -> {
                    val target = if (tombstone >= 0) tombstone else slot
                    keys[target] = threadToken
                    values[target] = contextToken
                    return true
                }
            }
        }
        return false
    }

    fun get(threadToken: Long): Long {
        if (threadToken <= 0L || threadToken == TOMBSTONE) return 0L
        val initial = index(threadToken)
        repeat(minOf(keys.size, MAX_PROBES)) { probe ->
            val slot = (initial + probe) % keys.size
            when (keys[slot]) {
                threadToken -> return values[slot]
                EMPTY -> return 0L
            }
        }
        return 0L
    }

    fun remove(threadToken: Long): Boolean {
        if (threadToken <= 0L || threadToken == TOMBSTONE) return false
        val initial = index(threadToken)
        repeat(minOf(keys.size, MAX_PROBES)) { probe ->
            val slot = (initial + probe) % keys.size
            when (keys[slot]) {
                threadToken -> {
                    keys[slot] = TOMBSTONE
                    values[slot] = 0L
                    return true
                }
                EMPTY -> return false
            }
        }
        return false
    }

    private fun index(token: Long): Int {
        val mixed = token xor (token ushr 33) xor (token shl 11)
        return ((mixed and Long.MAX_VALUE) % keys.size.toLong()).toInt()
    }

    private companion object {
        const val EMPTY = 0L
        const val TOMBSTONE = Long.MIN_VALUE
        const val MAX_PROBES = 32
    }
}
