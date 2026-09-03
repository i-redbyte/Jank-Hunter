package io.jankhunter.runtime.internal.io

import java.nio.charset.StandardCharsets

/** Owns segment-local dictionary IDs, front coding, tokenization and stable symbol aliases. */
internal class BinaryDictionaryEncoder(
    private val sink: BinaryEncodingSink,
    private val quality: LogQualityCounters,
    maxDictionaryEntries: Int,
    maxDictionaryValueBytes: Int,
) {
    private val dictionary = DictionaryIds(
        maxDictionaryEntries,
        minOf(maxDictionaryValueBytes, MAX_ENCODED_VALUE_BYTES),
    )
    private val lookupResult = DictionaryLookupResult()
    private val stableSymbols = StableSymbolRegistry()
    private val payload = BinaryPayload(128)
    private val frontState = DictionaryFrontState(DICTIONARY_KIND_COUNT)
    private var tokenState: SegmentDictionaryTokens? = null
    private var tokenPayload: BinaryPayload? = null

    fun symbolId(kind: Int, rawValue: String?): Long {
        dictionary.resolve(kind, rawValue, lookupResult)
        if (lookupResult.overflowed) quality.add(QualityCounterId.DICTIONARY_OVERFLOW_TOTAL)
        if (lookupResult.truncated) quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
        lookupResult.definition?.let(::writeDefinition)
        return lookupResult.id
    }

    fun defineStableSymbol(stableId: Long, rawName: String?): Long {
        val existing = stableSymbols.get(stableId)
        if (existing != null) {
            require(existing == rawName) {
                "stable symbol $stableId changed from '$existing' to '$rawName'"
            }
            return stableSymbols.alias(stableId)
        }
        val name = requireNotNull(rawName?.takeIf(String::isNotBlank)) {
            "stable symbol $stableId requires a readable embedded name"
        }
        val encoded = name.toByteArray(StandardCharsets.UTF_8)
        val bytes = if (encoded.size <= MAX_ENCODED_VALUE_BYTES) {
            encoded
        } else {
            quality.add(QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL)
            utf8Prefix(encoded, MAX_ENCODED_VALUE_BYTES)
        }
        val alias = stableSymbols.put(stableId, name)
        writeEncodedDefinition(BinaryLogWriter.DICT_STABLE_SYMBOL, alias, stableId, bytes)
        return alias
    }

    private fun writeDefinition(definition: DictionaryIds.Definition) {
        writeEncodedDefinition(
            definition.kind,
            definition.id,
            stableId = null,
            definition.value.toByteArray(StandardCharsets.UTF_8),
        )
    }

    private fun writeEncodedDefinition(kind: Int, alias: Long, stableId: Long?, bytes: ByteArray) {
        val prefix = frontState.commonPrefix(kind, bytes)
        val encoded = payload.clear()
            .uvarint(kind.toLong())
        if (stableId == null) {
            encoded.uvarint(alias)
        } else {
            // Stable aliases are assigned densely from one on both sides of the segment.
            encoded.fixedLongLe(stableId)
        }
        val tokenized = appendValue(kind, bytes, prefix, encoded)
        sink.emitDictionaryDefinition(encoded)
        if (tokenized) tokenState?.commit(bytes)
        frontState.commit(kind, bytes)
    }

    private fun appendValue(
        kind: Int,
        bytes: ByteArray,
        prefix: Int,
        target: BinaryPayload,
    ): Boolean {
        if (SegmentDictionaryTokens.supports(kind, bytes)) {
            val tokens = tokenState ?: SegmentDictionaryTokens().also { tokenState = it }
            val encoded = tokenPayload ?: BinaryPayload(256).also { tokenPayload = it }
            if (tokens.prepare(kind, bytes, prefix, encoded)) {
                target.bytes(encoded)
                return true
            }
        }
        target
            .uvarint(prefix.toLong() shl 1)
            .uvarint((bytes.size - prefix).toLong())
            .bytes(bytes, prefix, bytes.size - prefix)
        return false
    }

    private companion object {
        const val MAX_ENCODED_VALUE_BYTES = Jhlog.MAX_RAW_CHUNK_BYTES - 1024
        const val DICTIONARY_KIND_COUNT = BinaryLogWriter.DICT_ATTRIBUTE_VALUE + 1
    }
}
