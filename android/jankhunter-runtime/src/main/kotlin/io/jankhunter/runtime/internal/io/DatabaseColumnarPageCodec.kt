package io.jankhunter.runtime.internal.io

/** Reversible structure-of-arrays transform for database payloads inside one micro-page. */
internal class DatabaseColumnarPageCodec {
    private val databaseMasks = ByteArray(DATABASE_MASK_COUNT * MASK_STRIDE)
    private val transactionMasks = ByteArray(TRANSACTION_MASK_COUNT * MASK_STRIDE)
    private val databaseValues = LongArray(DATABASE_COLUMN_COUNT * Jhlog.MAX_MICRO_PAGE_ROWS)
    private val transactionValues = LongArray(TRANSACTION_COLUMN_COUNT * Jhlog.MAX_MICRO_PAGE_ROWS)
    private val databaseCounts = IntArray(DATABASE_COLUMN_COUNT)
    private val transactionCounts = IntArray(TRANSACTION_COLUMN_COUNT)
    private val reader = PayloadReader()
    private var databaseRows = 0
    private var transactionRows = 0

    fun encode(
        recordTypes: ByteArray,
        payloadArena: ByteArray,
        payloadLengths: IntArray,
        rowCount: Int,
        destination: BinaryPayload,
    ): Boolean {
        reset()
        var payloadOffset = 0
        var originalBytes = 0
        for (row in 0 until rowCount) {
            val length = payloadLengths[row]
            when (recordTypes[row].toInt() and 0xff) {
                Jhlog.TYPE_DATABASE -> {
                    originalBytes += length + PackedLongs.uvarintSize(length.toLong())
                    appendDatabase(payloadArena, payloadOffset, length)
                }
                Jhlog.TYPE_DATABASE_TRANSACTION -> {
                    originalBytes += length + PackedLongs.uvarintSize(length.toLong())
                    appendTransaction(payloadArena, payloadOffset, length)
                }
            }
            payloadOffset += length
        }
        if (databaseRows + transactionRows == 0) return false
        encodeColumns(destination.clear())
        val columnarWireBytes = destination.size + PackedLongs.uvarintSize(destination.size.toLong()) +
            databaseRows + transactionRows
        val rowWireBytes = originalBytes + EMPTY_DATABASE_SECTION_HEADER_BYTES
        if (columnarWireBytes < rowWireBytes) return true
        destination.clear()
        return false
    }

    private fun appendDatabase(bytes: ByteArray, offset: Int, length: Int) {
        val row = databaseRows
        reader.reset(bytes, offset, length)
        val descriptorToken = reader.read()
        check(descriptorToken != 0L)
        addDatabase(DATABASE_COLUMN_DESCRIPTOR, descriptorToken ushr 1)
        if (descriptorToken and 1L != 0L) {
            setMask(databaseMasks, DATABASE_MASK_DEFINITION, row)
            for (column in DATABASE_COLUMN_QUERY..DATABASE_COLUMN_BOUNDARY) addDatabase(column, reader.read())
        }
        addDatabase(DATABASE_COLUMN_OUTCOME, reader.read())
        addDatabase(DATABASE_COLUMN_DURATION, reader.read())
        val presence = reader.read()
        check(presence and DATABASE_KNOWN_PRESENCE.inv() == 0L)
        if (presence and DATABASE_PRESENCE_FAILURE != 0L) {
            setMask(databaseMasks, DATABASE_MASK_FAILURE, row)
            addDatabase(DATABASE_COLUMN_FAILURE, reader.read())
        }
        if (presence and DATABASE_PRESENCE_RESULT != 0L) {
            setMask(databaseMasks, DATABASE_MASK_RESULT, row)
            addDatabase(DATABASE_COLUMN_RESULT_KIND, reader.read())
            addDatabase(DATABASE_COLUMN_RESULT_BUCKET, reader.read())
        }
        if (presence and DATABASE_PRESENCE_TRANSACTION != 0L) {
            setMask(databaseMasks, DATABASE_MASK_TRANSACTION, row)
            addDatabase(DATABASE_COLUMN_TRANSACTION, reader.read())
        }
        if (presence and DATABASE_PRESENCE_STATEMENT != 0L) {
            setMask(databaseMasks, DATABASE_MASK_STATEMENT, row)
            addDatabase(DATABASE_COLUMN_STATEMENT, reader.read())
        }
        if (presence and DATABASE_PRESENCE_PHASES != 0L) {
            setMask(databaseMasks, DATABASE_MASK_PHASES, row)
            val phaseMask = reader.read()
            addDatabase(DATABASE_COLUMN_PHASE_MASK, phaseMask)
            if (phaseMask and Jhlog.DATABASE_PHASE_POOL_WAIT != 0L) {
                setMask(databaseMasks, DATABASE_MASK_POOL_WAIT, row)
                addDatabase(DATABASE_COLUMN_POOL_WAIT, reader.read())
            }
            if (phaseMask and Jhlog.DATABASE_PHASE_LOCK_WAIT != 0L) {
                setMask(databaseMasks, DATABASE_MASK_LOCK_WAIT, row)
                addDatabase(DATABASE_COLUMN_LOCK_WAIT, reader.read())
            }
            if (phaseMask and Jhlog.DATABASE_PHASE_EXECUTE != 0L) {
                setMask(databaseMasks, DATABASE_MASK_EXECUTE, row)
                addDatabase(DATABASE_COLUMN_EXECUTE, reader.read())
            }
            if (phaseMask and Jhlog.DATABASE_PHASE_MATERIALIZE != 0L) {
                setMask(databaseMasks, DATABASE_MASK_MATERIALIZE, row)
                addDatabase(DATABASE_COLUMN_MATERIALIZE, reader.read())
            }
        }
        check(reader.exhausted())
        databaseRows++
    }

    private fun appendTransaction(bytes: ByteArray, offset: Int, length: Int) {
        val row = transactionRows
        reader.reset(bytes, offset, length)
        addTransaction(TRANSACTION_COLUMN_SOURCE, reader.read())
        addTransaction(TRANSACTION_COLUMN_ID, reader.read())
        when (reader.read()) {
            Jhlog.DATABASE_TRANSACTION_BEGIN -> Unit
            Jhlog.DATABASE_TRANSACTION_TERMINAL -> setMask(transactionMasks, TRANSACTION_MASK_TERMINAL, row)
            else -> error("Unsupported database transaction stage")
        }
        val presence = reader.read()
        check(presence and TRANSACTION_KNOWN_PRESENCE.inv() == 0L)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_PARENT, TRANSACTION_MASK_PARENT, TRANSACTION_COLUMN_PARENT, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_MODE, TRANSACTION_MASK_MODE, TRANSACTION_COLUMN_MODE, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_OUTCOME, TRANSACTION_MASK_OUTCOME, TRANSACTION_COLUMN_OUTCOME, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_FAILURE, TRANSACTION_MASK_FAILURE, TRANSACTION_COLUMN_FAILURE, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_DURATION, TRANSACTION_MASK_DURATION, TRANSACTION_COLUMN_DURATION, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_STATEMENTS, TRANSACTION_MASK_STATEMENTS, TRANSACTION_COLUMN_STATEMENTS, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_READS, TRANSACTION_MASK_READS, TRANSACTION_COLUMN_READS, row)
        appendTransactionOptional(presence, TRANSACTION_PRESENCE_WRITES, TRANSACTION_MASK_WRITES, TRANSACTION_COLUMN_WRITES, row)
        check(reader.exhausted())
        transactionRows++
    }

    private fun appendTransactionOptional(presence: Long, bit: Long, mask: Int, column: Int, row: Int) {
        if (presence and bit == 0L) return
        setMask(transactionMasks, mask, row)
        addTransaction(column, reader.read())
    }

    private fun encodeColumns(destination: BinaryPayload) {
        destination
            .uvarint(DATABASE_PAGE_SCHEMA)
            .uvarint(databaseRows.toLong())
            .uvarint(transactionRows.toLong())
        val databaseMaskBytes = (databaseRows + 7) / 8
        for (mask in 0 until DATABASE_MASK_COUNT) {
            destination.bytes(databaseMasks, mask * MASK_STRIDE, databaseMaskBytes)
        }
        val transactionMaskBytes = (transactionRows + 7) / 8
        for (mask in 0 until TRANSACTION_MASK_COUNT) {
            destination.bytes(transactionMasks, mask * MASK_STRIDE, transactionMaskBytes)
        }
        for (column in 0 until DATABASE_COLUMN_COUNT) {
            encodeColumn(databaseValues, column, databaseCounts[column], destination)
        }
        for (column in 0 until TRANSACTION_COLUMN_COUNT) {
            encodeColumn(transactionValues, column, transactionCounts[column], destination)
        }
    }

    private fun encodeColumn(values: LongArray, column: Int, count: Int, destination: BinaryPayload) {
        if (count == 0) return
        val base = column * Jhlog.MAX_MICRO_PAGE_ROWS
        var minimum = values[base]
        var maximum = minimum
        var rawBytes = 0
        for (index in 0 until count) {
            val value = values[base + index]
            if (java.lang.Long.compareUnsigned(value, minimum) < 0) minimum = value
            if (java.lang.Long.compareUnsigned(value, maximum) > 0) maximum = value
            rawBytes += PackedLongs.uvarintSize(value)
        }
        if (minimum == maximum) {
            destination.uvarint(PACKED_COLUMN_CONSTANT).uvarint(minimum)
            return
        }
        val width = PackedLongs.bitWidth(maximum - minimum)
        val packedBytes = (count * width + 7) / 8
        val frameBytes = 1 + PackedLongs.uvarintSize(minimum) + 1 + packedBytes
        if (frameBytes >= 1 + rawBytes) {
            destination.uvarint(PACKED_COLUMN_UVARINT)
            for (index in 0 until count) destination.uvarint(values[base + index])
            return
        }
        destination.uvarint(PACKED_COLUMN_FOR).uvarint(minimum).uvarint(width.toLong())
        PackedLongs.writeFrame(values, base, count, minimum, width, destination)
    }

    private fun addDatabase(column: Int, value: Long) {
        val count = databaseCounts[column]
        databaseValues[column * Jhlog.MAX_MICRO_PAGE_ROWS + count] = value
        databaseCounts[column] = count + 1
    }

    private fun addTransaction(column: Int, value: Long) {
        val count = transactionCounts[column]
        transactionValues[column * Jhlog.MAX_MICRO_PAGE_ROWS + count] = value
        transactionCounts[column] = count + 1
    }

    private fun setMask(masks: ByteArray, mask: Int, row: Int) {
        val index = mask * MASK_STRIDE + row / Byte.SIZE_BITS
        masks[index] = (masks[index].toInt() or (1 shl (row % Byte.SIZE_BITS))).toByte()
    }

    private fun reset() {
        databaseRows = 0
        transactionRows = 0
        databaseCounts.fill(0)
        transactionCounts.fill(0)
        databaseMasks.fill(0)
        transactionMasks.fill(0)
    }

    private class PayloadReader {
        private var bytes = ByteArray(0)
        private var offset = 0
        private var end = 0

        fun reset(bytes: ByteArray, offset: Int, length: Int) {
            this.bytes = bytes
            this.offset = offset
            end = offset + length
        }

        fun read(): Long {
            var value = 0L
            var shift = 0
            while (shift < Long.SIZE_BITS) {
                check(offset < end)
                val current = bytes[offset++].toInt() and 0xff
                value = value or ((current and 0x7f).toLong() shl shift)
                if (current and 0x80 == 0) return value
                shift += 7
            }
            error("Invalid database payload varint")
        }

        fun exhausted(): Boolean = offset == end
    }

    private companion object {
        const val MASK_STRIDE = (Jhlog.MAX_MICRO_PAGE_ROWS + 7) / 8
        const val DATABASE_PAGE_SCHEMA = 1L
        const val EMPTY_DATABASE_SECTION_HEADER_BYTES = 1

        const val PACKED_COLUMN_CONSTANT = 0L
        const val PACKED_COLUMN_FOR = 1L
        const val PACKED_COLUMN_UVARINT = 2L

        const val DATABASE_PRESENCE_FAILURE = 1L shl 0
        const val DATABASE_PRESENCE_RESULT = 1L shl 1
        const val DATABASE_PRESENCE_TRANSACTION = 1L shl 2
        const val DATABASE_PRESENCE_STATEMENT = 1L shl 3
        const val DATABASE_PRESENCE_PHASES = 1L shl 4
        const val DATABASE_KNOWN_PRESENCE = (1L shl 5) - 1L

        const val TRANSACTION_PRESENCE_PARENT = 1L shl 0
        const val TRANSACTION_PRESENCE_MODE = 1L shl 1
        const val TRANSACTION_PRESENCE_OUTCOME = 1L shl 2
        const val TRANSACTION_PRESENCE_FAILURE = 1L shl 3
        const val TRANSACTION_PRESENCE_DURATION = 1L shl 4
        const val TRANSACTION_PRESENCE_STATEMENTS = 1L shl 5
        const val TRANSACTION_PRESENCE_READS = 1L shl 6
        const val TRANSACTION_PRESENCE_WRITES = 1L shl 7
        const val TRANSACTION_KNOWN_PRESENCE = (1L shl 8) - 1L

        const val DATABASE_MASK_DEFINITION = 0
        const val DATABASE_MASK_FAILURE = 1
        const val DATABASE_MASK_RESULT = 2
        const val DATABASE_MASK_TRANSACTION = 3
        const val DATABASE_MASK_STATEMENT = 4
        const val DATABASE_MASK_PHASES = 5
        const val DATABASE_MASK_POOL_WAIT = 6
        const val DATABASE_MASK_LOCK_WAIT = 7
        const val DATABASE_MASK_EXECUTE = 8
        const val DATABASE_MASK_MATERIALIZE = 9
        const val DATABASE_MASK_COUNT = 10

        const val TRANSACTION_MASK_TERMINAL = 0
        const val TRANSACTION_MASK_PARENT = 1
        const val TRANSACTION_MASK_MODE = 2
        const val TRANSACTION_MASK_OUTCOME = 3
        const val TRANSACTION_MASK_FAILURE = 4
        const val TRANSACTION_MASK_DURATION = 5
        const val TRANSACTION_MASK_STATEMENTS = 6
        const val TRANSACTION_MASK_READS = 7
        const val TRANSACTION_MASK_WRITES = 8
        const val TRANSACTION_MASK_COUNT = 9

        const val DATABASE_COLUMN_DESCRIPTOR = 0
        const val DATABASE_COLUMN_QUERY = 1
        const val DATABASE_COLUMN_SOURCE = 2
        const val DATABASE_COLUMN_FINGERPRINT = 3
        const val DATABASE_COLUMN_FRAMEWORK = 4
        const val DATABASE_COLUMN_OPERATION = 5
        const val DATABASE_COLUMN_BOUNDARY = 6
        const val DATABASE_COLUMN_OUTCOME = 7
        const val DATABASE_COLUMN_DURATION = 8
        const val DATABASE_COLUMN_FAILURE = 9
        const val DATABASE_COLUMN_RESULT_KIND = 10
        const val DATABASE_COLUMN_RESULT_BUCKET = 11
        const val DATABASE_COLUMN_TRANSACTION = 12
        const val DATABASE_COLUMN_STATEMENT = 13
        const val DATABASE_COLUMN_PHASE_MASK = 14
        const val DATABASE_COLUMN_POOL_WAIT = 15
        const val DATABASE_COLUMN_LOCK_WAIT = 16
        const val DATABASE_COLUMN_EXECUTE = 17
        const val DATABASE_COLUMN_MATERIALIZE = 18
        const val DATABASE_COLUMN_COUNT = 19

        const val TRANSACTION_COLUMN_SOURCE = 0
        const val TRANSACTION_COLUMN_ID = 1
        const val TRANSACTION_COLUMN_PARENT = 2
        const val TRANSACTION_COLUMN_MODE = 3
        const val TRANSACTION_COLUMN_OUTCOME = 4
        const val TRANSACTION_COLUMN_FAILURE = 5
        const val TRANSACTION_COLUMN_DURATION = 6
        const val TRANSACTION_COLUMN_STATEMENTS = 7
        const val TRANSACTION_COLUMN_READS = 8
        const val TRANSACTION_COLUMN_WRITES = 9
        const val TRANSACTION_COLUMN_COUNT = 10
    }
}
