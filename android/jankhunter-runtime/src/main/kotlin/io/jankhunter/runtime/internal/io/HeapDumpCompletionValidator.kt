package io.jankhunter.runtime.internal.io

/** Checks stream framing and dump completion, not semantic validity of the object graph. */
internal class HeapDumpCompletionValidator {
    private val header = ByteArray(31)
    private val record = ByteArray(9)
    private var headerSize = 0
    private var recordSize = 0
    private var remaining = 0L
    private var invalid = false
    private var sawHeap = false
    private var segmentedOpen = false

    fun accept(bytes: ByteArray, offset: Int, length: Int) {
        var index = offset
        val end = offset + length
        while (index < end && !invalid) {
            when {
                headerSize < header.size -> {
                    header[headerSize++] = bytes[index++]
                    if (headerSize == header.size) validateHeader()
                }
                remaining > 0L -> {
                    val consumed = minOf(remaining, (end - index).toLong()).toInt()
                    remaining -= consumed
                    index += consumed
                }
                else -> {
                    record[recordSize++] = bytes[index++]
                    if (recordSize == record.size) beginRecord()
                }
            }
        }
    }

    fun isComplete(): Boolean = !invalid && headerSize == header.size && recordSize == 0 &&
        remaining == 0L && sawHeap && !segmentedOpen

    private fun validateHeader() {
        val signature = String(header, 0, 19, Charsets.US_ASCII)
        invalid = signature != "JAVA PROFILE 1.0.2\u0000" && signature != "JAVA PROFILE 1.0.3\u0000"
        val idSize = unsignedInt(header, 19)
        if (idSize != 4L && idSize != 8L) invalid = true
    }

    private fun beginRecord() {
        val tag = record[0].toInt() and 0xff
        remaining = unsignedInt(record, 5)
        recordSize = 0
        when (tag) {
            0x0c -> {
                if (segmentedOpen || remaining == 0L) invalid = true
                sawHeap = true
            }
            0x1c -> {
                if (remaining == 0L) invalid = true
                sawHeap = true
                segmentedOpen = true
            }
            0x2c -> {
                if (!segmentedOpen || remaining != 0L) invalid = true
                segmentedOpen = false
            }
        }
    }

    private fun unsignedInt(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        for (index in offset until offset + 4) value = (value shl 8) or (bytes[index].toLong() and 0xff)
        return value
    }
}
