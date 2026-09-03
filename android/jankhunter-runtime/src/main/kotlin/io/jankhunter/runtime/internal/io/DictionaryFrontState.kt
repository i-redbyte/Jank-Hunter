package io.jankhunter.runtime.internal.io

/** Per-kind UTF-8 front-coding state with bounded lazy byte arenas. */
internal class DictionaryFrontState(
    kindCount: Int,
) {
    private val previous = arrayOfNulls<ByteArray>(kindCount)
    private val previousSizes = IntArray(kindCount)

    fun commonPrefix(kind: Int, value: ByteArray): Int {
        require(kind in previous.indices)
        val prior = previous[kind] ?: return 0
        val limit = minOf(previousSizes[kind], value.size)
        var prefix = 0
        while (prefix < limit && prior[prefix] == value[prefix]) prefix++
        if (prefix == value.size || prefix == previousSizes[kind]) return prefix
        while (prefix > 0 && value[prefix].toInt() and UTF8_CONTINUATION_MASK == UTF8_CONTINUATION_TAG) {
            prefix--
        }
        return prefix
    }

    fun commit(kind: Int, value: ByteArray) {
        require(kind in previous.indices)
        var target = previous[kind]
        if (target == null || target.size < value.size) {
            var capacity = target?.size ?: INITIAL_VALUE_BYTES
            while (capacity < value.size) capacity = capacity shl 1
            target = ByteArray(capacity)
            previous[kind] = target
        }
        value.copyInto(target, endIndex = value.size)
        previousSizes[kind] = value.size
    }

    private companion object {
        const val INITIAL_VALUE_BYTES = 64
        const val UTF8_CONTINUATION_MASK = 0xc0
        const val UTF8_CONTINUATION_TAG = 0x80
    }
}
