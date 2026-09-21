package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.saturatingAdd

/**
 * Bounded allocation-free-on-steady-state columnar staging page for semantic records.
 *
 * Payload bytes stay row-oriented because their schemas differ, while high-frequency envelope
 * fields are transposed into bit masks and primitive streams. Each sufficiently large section is
 * independently considered for rANS and stays raw unless its complete entropy frame is smaller.
 */
internal class ColumnarMicroPage {
    private val recordTypes = ByteArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val metadata = ByteArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val attributes = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val elapsedUs = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val threadIds = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val screenIds = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val ownerIds = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val stableOwnerAliases = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val operationIds = LongArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val payloadLengths = IntArray(Jhlog.MAX_MICRO_PAGE_ROWS)
    private val semanticCounts = LongArray(Jhlog.TYPE_BINDER_TRANSACTION + 1)
    private var payloadArena = ByteArray(INITIAL_PAYLOAD_BYTES)
    private var payloadBytes = 0

    private val typesSection = BinaryPayload(80)
    private val masksSection = BinaryPayload(MAX_MASK_BYTES)
    private val timeSection = BinaryPayload(256)
    private val threadSection = BinaryPayload(128)
    private val contextSection = BinaryPayload(256)
    private val attributesSection = BinaryPayload(128)
    private val lengthsSection = BinaryPayload(128)
    private val compactPayloadSection = BinaryPayload(INITIAL_PAYLOAD_BYTES)
    private val databaseSection = BinaryPayload(256)
    private var databaseCodec: DatabaseColumnarPageCodec? = null
    private var entropySections: Array<BinaryPayload>? = null
    private var entropyCodec: RansSectionCodec? = null
    private val masks = ByteArray(MAX_MASK_BYTES)

    var size: Int = 0
        private set

    fun canAppend(payloadSize: Int): Boolean {
        return size < Jhlog.MAX_MICRO_PAGE_ROWS &&
            payloadSize >= 0 &&
            (payloadBytes.toLong() + payloadSize.toLong()) <= MAX_PAYLOAD_BYTES
    }

    fun projectedRecordBytes(additionalPayloadSize: Int): Int {
        val rows = size + 1
        val bytes = payloadBytes.toLong() + additionalPayloadSize.toLong() +
            rows.toLong() * MAX_METADATA_BYTES_PER_ROW + PAGE_FIXED_BYTES
        return bytes.coerceAtMost(Int.MAX_VALUE.toLong()).toInt()
    }

    fun append(
        recordType: Int,
        attributes: Long,
        payload: BinaryPayload,
        context: BinaryRecordContext?,
        producer: ProducerMetadataBuffer?,
        semanticEventCount: Long,
    ) {
        check(canAppend(payload.size))
        val index = size++
        recordTypes[index] = recordType.toByte()
        var flags = 0
        if (producer != null) {
            flags = flags or HAS_TIME or HAS_THREAD
            elapsedUs[index] = producer.elapsedUs
            threadIds[index] = producer.threadId
        }
        if (context != null) {
            flags = flags or HAS_CONTEXT
            screenIds[index] = context.screenId
            ownerIds[index] = context.ownerId
            stableOwnerAliases[index] = context.stableOwnerAlias
            operationIds[index] = context.operationId
            if (context.hasStableOwner) flags = flags or HAS_STABLE_OWNER
        }
        if (attributes != 0L) {
            flags = flags or HAS_ATTRIBUTES
            this.attributes[index] = attributes
        }
        metadata[index] = flags.toByte()
        payloadLengths[index] = payload.size
        ensurePayloadCapacity(payloadBytes + payload.size)
        payloadBytes = payload.copyTo(payloadArena, payloadBytes)
        semanticCounts[recordType] = saturatingAdd(semanticCounts[recordType], semanticEventCount)
    }

    fun encodeTo(destination: BinaryPayload, entropyEnabled: Boolean = false): BinaryPayload {
        check(size > 0)
        encodeTypes()
        encodeMetadata()
        val databaseColumns = hasDatabaseRows() && databaseCodec().encode(
            recordTypes,
            payloadArena,
            payloadLengths,
            size,
            databaseSection,
        )
        encodePayloads(databaseColumns)
        var codecMask = 0
        if (entropyEnabled) {
            val codec = entropyCodec()
            val encoded = entropySections()
            for (section in 0 until SECTION_COUNT) {
                if (codec.encode(sectionBytes(section), sectionSize(section), encoded[section])) {
                    codecMask = codecMask or (1 shl section)
                }
            }
        }
        destination.clear()
        if (codecMask == 0) {
            destination.uvarint(size.toLong())
        } else {
            destination.uvarint(ENTROPY_PAGE_MARKER).uvarint(size.toLong()).uvarint(codecMask.toLong())
        }
        for (section in 0 until SECTION_COUNT) {
            writeSection(destination, section, codecMask and (1 shl section) != 0)
        }
        return destination
    }

    fun addSemanticCountsTo(target: LongArray) {
        for (recordType in semanticCounts.indices) {
            val count = semanticCounts[recordType]
            if (count != 0L) target[recordType] = saturatingAdd(target[recordType], count)
        }
    }

    fun rejectSemanticCounts(quality: LogQualityCounters, reason: Int) {
        for (recordType in semanticCounts.indices) {
            val count = semanticCounts[recordType]
            if (count != 0L) quality.addRejected(recordType, reason, count)
        }
    }

    fun reset() {
        size = 0
        payloadBytes = 0
        semanticCounts.fill(0L)
    }

    private fun encodeTypes() {
        val output = typesSection.clear()
        var bits = 0
        var buffered = 0L
        for (index in 0 until size) {
            buffered = buffered or ((recordTypes[index].toLong() - 1L) shl bits)
            bits += TYPE_BITS
            while (bits >= Byte.SIZE_BITS) {
                output.byte(buffered.toInt())
                buffered = buffered ushr Byte.SIZE_BITS
                bits -= Byte.SIZE_BITS
            }
        }
        if (bits != 0) output.byte(buffered.toInt())
    }

    private fun encodeMetadata() {
        val maskBytes = (size + 7) / 8
        val usedMaskBytes = maskBytes * MASK_COUNT
        masks.fill(0, 0, usedMaskBytes)
        timeSection.clear()
        threadSection.clear()
        contextSection.clear()
        attributesSection.clear()
        var hasTimedValue = false
        var lastElapsedUs = 0L
        var hasThreadValue = false
        var lastThreadId = 0L
        var hasContextValue = false
        var lastContextIndex = 0
        for (index in 0 until size) {
            val flags = metadata[index].toInt()
            if (flags and HAS_TIME != 0) {
                setMask(maskBytes, MASK_TIME_PRESENCE, index)
                val current = elapsedUs[index]
                if (hasTimedValue) timeSection.svarint(current - lastElapsedUs) else timeSection.uvarint(current)
                lastElapsedUs = current
                hasTimedValue = true
            }
            if (flags and HAS_THREAD != 0) {
                setMask(maskBytes, MASK_THREAD_PRESENCE, index)
                val current = threadIds[index]
                if (!hasThreadValue || current != lastThreadId) {
                    setMask(maskBytes, MASK_THREAD_CHANGE, index)
                    threadSection.uvarint(current)
                    lastThreadId = current
                    hasThreadValue = true
                }
            }
            if (flags and HAS_CONTEXT != 0) {
                setMask(maskBytes, MASK_CONTEXT_PRESENCE, index)
                if (!hasContextValue || !sameContext(index, lastContextIndex)) {
                    setMask(maskBytes, MASK_CONTEXT_CHANGE, index)
                    writeContext(index)
                    lastContextIndex = index
                    hasContextValue = true
                }
            }
            if (flags and HAS_ATTRIBUTES != 0) {
                setMask(maskBytes, MASK_ATTRIBUTES_PRESENCE, index)
                attributesSection.uvarint(attributes[index])
            }
        }
        masksSection.clear().bytes(masks, usedMaskBytes)
    }

    private fun writeContext(index: Int) {
        var presence = 0L
        if (screenIds[index] != 0L) presence = presence or Jhlog.CONTEXT_SCREEN
        if (ownerIds[index] != 0L || metadata[index].toInt() and HAS_STABLE_OWNER != 0) {
            presence = presence or Jhlog.CONTEXT_OWNER
        }
        if (operationIds[index] != 0L) presence = presence or Jhlog.CONTEXT_OPERATION
        contextSection.uvarint(presence)
        if (presence and Jhlog.CONTEXT_SCREEN != 0L) contextSection.symbolRef(screenIds[index])
        if (presence and Jhlog.CONTEXT_OWNER != 0L) {
            if (metadata[index].toInt() and HAS_STABLE_OWNER != 0) {
                contextSection.stableSymbolAlias(stableOwnerAliases[index])
            } else {
                contextSection.symbolRef(ownerIds[index])
            }
        }
        if (presence and Jhlog.CONTEXT_OPERATION != 0L) contextSection.uvarint(operationIds[index])
    }

    private fun sameContext(left: Int, right: Int): Boolean {
        return screenIds[left] == screenIds[right] &&
            ownerIds[left] == ownerIds[right] &&
            stableOwnerAliases[left] == stableOwnerAliases[right] &&
            (metadata[left].toInt() and HAS_STABLE_OWNER) ==
            (metadata[right].toInt() and HAS_STABLE_OWNER) &&
            operationIds[left] == operationIds[right]
    }

    private fun encodePayloads(databaseColumns: Boolean) {
        lengthsSection.clear()
        compactPayloadSection.clear()
        var offset = 0
        for (index in 0 until size) {
            val length = payloadLengths[index]
            val columnar = databaseColumns && isDatabaseType(recordTypes[index].toInt() and 0xff)
            lengthsSection.uvarint(if (columnar) 0L else length.toLong())
            if (!columnar) compactPayloadSection.bytes(payloadArena, offset, length)
            offset += length
        }
    }

    private fun setMask(maskBytes: Int, mask: Int, row: Int) {
        val index = mask * maskBytes + row / Byte.SIZE_BITS
        masks[index] = masks[index].toInt().or(1 shl (row % Byte.SIZE_BITS)).toByte()
    }

    private fun ensurePayloadCapacity(required: Int) {
        if (required <= payloadArena.size) return
        var capacity = payloadArena.size
        while (capacity < required) capacity = minOf(MAX_PAYLOAD_BYTES, capacity shl 1)
        payloadArena = payloadArena.copyOf(capacity)
    }

    private fun writeSection(destination: BinaryPayload, section: Int, compressed: Boolean) {
        val bytes = sectionBytes(section)
        val length = sectionSize(section)
        destination.uvarint(length.toLong())
        if (compressed) {
            val encoded = checkNotNull(entropySections)[section]
            destination.uvarint(encoded.size.toLong()).bytes(encoded)
        } else {
            destination.bytes(bytes, length)
        }
    }

    private fun sectionBytes(section: Int): ByteArray = when (section) {
        SECTION_TYPES -> typesSection.prefixBuffer()
        SECTION_MASKS -> masksSection.prefixBuffer()
        SECTION_TIME -> timeSection.prefixBuffer()
        SECTION_THREAD -> threadSection.prefixBuffer()
        SECTION_CONTEXT -> contextSection.prefixBuffer()
        SECTION_ATTRIBUTES -> attributesSection.prefixBuffer()
        SECTION_LENGTHS -> lengthsSection.prefixBuffer()
        SECTION_PAYLOAD -> compactPayloadSection.prefixBuffer()
        SECTION_DATABASE -> databaseSection.prefixBuffer()
        else -> error("Invalid micro-page section $section")
    }

    private fun sectionSize(section: Int): Int = when (section) {
        SECTION_TYPES -> typesSection.size
        SECTION_MASKS -> masksSection.size
        SECTION_TIME -> timeSection.size
        SECTION_THREAD -> threadSection.size
        SECTION_CONTEXT -> contextSection.size
        SECTION_ATTRIBUTES -> attributesSection.size
        SECTION_LENGTHS -> lengthsSection.size
        SECTION_PAYLOAD -> compactPayloadSection.size
        SECTION_DATABASE -> databaseSection.size
        else -> error("Invalid micro-page section $section")
    }

    private fun entropyCodec(): RansSectionCodec {
        val current = entropyCodec
        if (current != null) return current
        return RansSectionCodec().also { entropyCodec = it }
    }

    private fun entropySections(): Array<BinaryPayload> {
        val current = entropySections
        if (current != null) return current
        return Array(SECTION_COUNT) { BinaryPayload(256) }.also { entropySections = it }
    }

    private fun hasDatabaseRows(): Boolean {
        for (index in 0 until size) {
            if (isDatabaseType(recordTypes[index].toInt() and 0xff)) return true
        }
        databaseSection.clear()
        return false
    }

    private fun isDatabaseType(recordType: Int): Boolean {
        return recordType == Jhlog.TYPE_DATABASE || recordType == Jhlog.TYPE_DATABASE_TRANSACTION
    }

    private fun databaseCodec(): DatabaseColumnarPageCodec {
        val current = databaseCodec
        if (current != null) return current
        return DatabaseColumnarPageCodec().also { databaseCodec = it }
    }

    private companion object {
        const val INITIAL_PAYLOAD_BYTES = 8 * 1024
        const val MAX_PAYLOAD_BYTES = 48 * 1024
        const val MAX_METADATA_BYTES_PER_ROW = 80L
        const val PAGE_FIXED_BYTES = 128L
        const val TYPE_BITS = 5
        const val MASK_COUNT = 6
        const val MAX_MASK_BYTES = MASK_COUNT * ((Jhlog.MAX_MICRO_PAGE_ROWS + 7) / 8)

        const val SECTION_TYPES = 0
        const val SECTION_MASKS = 1
        const val SECTION_TIME = 2
        const val SECTION_THREAD = 3
        const val SECTION_CONTEXT = 4
        const val SECTION_ATTRIBUTES = 5
        const val SECTION_LENGTHS = 6
        const val SECTION_PAYLOAD = 7
        const val SECTION_DATABASE = 8
        const val SECTION_COUNT = 9
        const val ENTROPY_PAGE_MARKER = 0L

        const val HAS_TIME = 1 shl 0
        const val HAS_THREAD = 1 shl 1
        const val HAS_CONTEXT = 1 shl 2
        const val HAS_ATTRIBUTES = 1 shl 3
        const val HAS_STABLE_OWNER = 1 shl 4

        const val MASK_TIME_PRESENCE = 0
        const val MASK_THREAD_PRESENCE = 1
        const val MASK_THREAD_CHANGE = 2
        const val MASK_CONTEXT_PRESENCE = 3
        const val MASK_CONTEXT_CHANGE = 4
        const val MASK_ATTRIBUTES_PRESENCE = 5
    }
}
