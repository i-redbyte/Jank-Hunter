package io.jankhunter.runtime.internal.io

/** Bounded segment-local token registry for package, class, and method dictionary values. */
internal class SegmentDictionaryTokens {
    private val slots = IntArray(TABLE_SIZE)
    private val offsets = IntArray(MAX_TOKENS + 1)
    private val lengths = IntArray(MAX_TOKENS + 1)
    private val pendingOffsets = IntArray(MAX_ATOMS)
    private val pendingLengths = IntArray(MAX_ATOMS)
    private var arena = ByteArray(INITIAL_ARENA_BYTES)
    private var arenaSize = 0
    private var tokenCount = 0
    private var pendingCount = 0
    private var pendingBytes = 0
    private var pendingBalanceGain = 0
    private var balance = 0

    fun prepare(
        kind: Int,
        value: ByteArray,
        frontPrefix: Int,
        destination: BinaryPayload,
    ): Boolean {
        pendingCount = 0
        pendingBytes = 0
        pendingBalanceGain = 0
        if (!supports(kind, value)) return false
        val atomCount = atomCount(value)
        if (atomCount == 0 || atomCount > MAX_ATOMS) return false
        destination.clear().uvarint((atomCount.toLong() shl 1) or TOKEN_LAYOUT_BIT)
        var offset = 0
        while (offset < value.size) {
            if (!isIdentifierByte(value[offset])) {
                appendSeparator(value[offset], destination)
                offset++
                continue
            }
            var end = offset + 1
            while (end < value.size && isIdentifierByte(value[end])) end++
            appendIdentifier(value, offset, end, destination)
            offset = end
        }
        val frontBytes = PackedLongs.uvarintSize(frontPrefix.toLong() shl 1) +
            PackedLongs.uvarintSize((value.size - frontPrefix).toLong()) + value.size - frontPrefix
        val gain = frontBytes - destination.size
        if (balance + gain < -MAX_BOOTSTRAP_DEBT) {
            pendingCount = 0
            pendingBytes = 0
            destination.clear()
            return false
        }
        pendingBalanceGain = gain
        return true
    }

    fun commit(value: ByteArray) {
        ensureArenaCapacity(arenaSize + pendingBytes)
        for (index in 0 until pendingCount) {
            val offset = pendingOffsets[index]
            val length = pendingLengths[index]
            val id = ++tokenCount
            offsets[id] = arenaSize
            lengths[id] = length
            value.copyInto(arena, destinationOffset = arenaSize, startIndex = offset, endIndex = offset + length)
            arenaSize += length
            insert(id, hash(value, offset, length))
        }
        balance += pendingBalanceGain
        pendingCount = 0
        pendingBytes = 0
        pendingBalanceGain = 0
    }

    private fun appendIdentifier(value: ByteArray, offset: Int, end: Int, destination: BinaryPayload) {
        val length = end - offset
        val hash = hash(value, offset, length)
        var id = find(value, offset, length, hash)
        if (id == 0) id = findPending(value, offset, length)
        if (id != 0) {
            destination.uvarint(id.toLong() shl ATOM_TAG_BITS)
            return
        }
        if (tokenCount + pendingCount >= MAX_TOKENS || arenaSize + pendingBytes + length > MAX_TOKEN_BYTES) {
            destination.uvarint((length.toLong() shl ATOM_TAG_BITS) or ATOM_LITERAL).bytes(value, offset, length)
            return
        }
        id = tokenCount + pendingCount + 1
        destination
            .uvarint((id.toLong() shl ATOM_TAG_BITS) or ATOM_DEFINITION)
            .uvarint(length.toLong())
            .bytes(value, offset, length)
        pendingOffsets[pendingCount] = offset
        pendingLengths[pendingCount] = length
        pendingCount++
        pendingBytes += length
    }

    private fun appendSeparator(value: Byte, destination: BinaryPayload) {
        val separator = value.toInt() and 0xff
        val code = SEPARATORS.indexOf(separator.toChar())
        if (code >= 0) {
            destination.uvarint((code.toLong() shl ATOM_TAG_BITS) or ATOM_SEPARATOR)
        } else {
            destination.uvarint((1L shl ATOM_TAG_BITS) or ATOM_LITERAL).byte(separator)
        }
    }

    private fun find(value: ByteArray, offset: Int, length: Int, hash: Int): Int {
        var slot = spread(hash) and TABLE_MASK
        while (true) {
            val id = slots[slot]
            if (id == 0) return 0
            if (lengths[id] == length && equalsToken(id, value, offset, length)) return id
            slot = (slot + 1) and TABLE_MASK
        }
    }

    private fun findPending(value: ByteArray, offset: Int, length: Int): Int {
        for (index in 0 until pendingCount) {
            if (pendingLengths[index] != length) continue
            var byteIndex = 0
            while (byteIndex < length && value[pendingOffsets[index] + byteIndex] == value[offset + byteIndex]) {
                byteIndex++
            }
            if (byteIndex == length) return tokenCount + index + 1
        }
        return 0
    }

    private fun equalsToken(id: Int, value: ByteArray, offset: Int, length: Int): Boolean {
        val tokenOffset = offsets[id]
        for (index in 0 until length) {
            if (arena[tokenOffset + index] != value[offset + index]) return false
        }
        return true
    }

    private fun insert(id: Int, hash: Int) {
        var slot = spread(hash) and TABLE_MASK
        while (slots[slot] != 0) slot = (slot + 1) and TABLE_MASK
        slots[slot] = id
    }

    private fun ensureArenaCapacity(required: Int) {
        if (required <= arena.size) return
        var capacity = arena.size
        while (capacity < required) capacity = minOf(MAX_TOKEN_BYTES, capacity shl 1)
        arena = arena.copyOf(capacity)
    }

    private fun hash(value: ByteArray, offset: Int, length: Int): Int {
        var hash = FNV_OFFSET
        for (index in offset until offset + length) {
            hash = (hash xor (value[index].toLong() and 0xffL)) * FNV_PRIME
        }
        return (hash xor (hash ushr Int.SIZE_BITS)).toInt()
    }

    private fun spread(value: Int): Int {
        var mixed = value
        mixed = mixed xor (mixed ushr 16)
        mixed *= -0x7a143595
        mixed = mixed xor (mixed ushr 13)
        return mixed
    }

    internal companion object {
        private const val MAX_TOKENS = 8_192
        private const val MAX_TOKEN_BYTES = 256 * 1024
        private const val MAX_ATOMS = 256
        private const val MAX_BOOTSTRAP_DEBT = 1_024
        private const val TABLE_SIZE = MAX_TOKENS * 2
        private const val TABLE_MASK = TABLE_SIZE - 1
        private const val INITIAL_ARENA_BYTES = 8 * 1024
        private const val ATOM_TAG_BITS = 2
        private const val ATOM_DEFINITION = 1L
        private const val ATOM_SEPARATOR = 2L
        private const val ATOM_LITERAL = 3L
        private const val TOKEN_LAYOUT_BIT = 1L
        private const val FNV_OFFSET = -0x340d631b7bdddcdbL
        private const val FNV_PRIME = 0x100000001b3L
        private const val SEPARATORS = ".$/#()[];:<> ,-=?!@+*'\"\\&|"

        fun supports(kind: Int, value: ByteArray): Boolean {
            if (
                kind != BinaryLogWriter.DICT_OWNER && kind != BinaryLogWriter.DICT_CLASS &&
                kind != BinaryLogWriter.DICT_STACK && kind != BinaryLogWriter.DICT_LOG_SOURCE &&
                kind != BinaryLogWriter.DICT_STABLE_SYMBOL && kind != BinaryLogWriter.DICT_OPERATION
            ) {
                return false
            }
            for (byte in value) {
                when (byte.toInt()) {
                    '.'.code, '$'.code, '/'.code, '#'.code -> return true
                }
            }
            return false
        }

        private fun atomCount(value: ByteArray): Int {
            var atoms = 0
            var offset = 0
            while (offset < value.size) {
                atoms++
                if (!isIdentifierByte(value[offset])) {
                    offset++
                    continue
                }
                offset++
                while (offset < value.size && isIdentifierByte(value[offset])) offset++
            }
            return atoms
        }

        private fun isIdentifierByte(value: Byte): Boolean {
            val unsigned = value.toInt() and 0xff
            return unsigned in 'a'.code..'z'.code || unsigned in 'A'.code..'Z'.code ||
                unsigned in '0'.code..'9'.code || unsigned == '_'.code || unsigned >= 0x80
        }
    }
}
