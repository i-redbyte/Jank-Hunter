package io.jankhunter.gradle

import java.nio.charset.StandardCharsets
import java.security.MessageDigest

internal object JankHunterSymbolNamespace {
    data class Contract(
        val embeddedSymbolFormat: Int,
        val stableIdAlgorithm: String,
        val stableIdEncoding: String,
    )

    /**
     * Produces the global namespace for stable ASM symbols.
     *
     * Module, variant and source content are deliberately excluded: a stable ID already hashes the
     * canonical class, method and descriptor, while one process can execute instrumented code from
     * several Android modules. Only an incompatible embedded-symbol contract changes this
     * namespace.
     */
    fun current(): String = currentNamespace

    internal fun currentContract(): Contract {
        return Contract(
            embeddedSymbolFormat = EMBEDDED_SYMBOL_FORMAT,
            stableIdAlgorithm = OwnerIds.STABLE_ID_ALGORITHM,
            stableIdEncoding = OwnerIds.STABLE_ID_ENCODING,
        )
    }

    internal fun generate(contract: Contract): String {
        val digest = MessageDigest.getInstance("SHA-256")
        digest.field("domain", "$CONTRACT_DOMAIN.embedded-v${contract.embeddedSymbolFormat}")
        digest.field("embeddedSymbolFormat", contract.embeddedSymbolFormat.toString())
        digest.field("stableIdAlgorithm", contract.stableIdAlgorithm)
        digest.field("stableIdEncoding", contract.stableIdEncoding)
        val bytes = digest.digest()
        return buildString(SYMBOL_NAMESPACE_BYTES * 2) {
            repeat(SYMBOL_NAMESPACE_BYTES) { index ->
                val value = bytes[index].toInt() and 0xff
                append(LOWERCASE_HEX[value ushr 4])
                append(LOWERCASE_HEX[value and 0x0f])
            }
        }
    }

    private fun MessageDigest.field(name: String, value: String) {
        val nameBytes = name.toByteArray(StandardCharsets.UTF_8)
        val valueBytes = value.toByteArray(StandardCharsets.UTF_8)
        updateLength(nameBytes.size)
        update(nameBytes)
        updateLength(valueBytes.size)
        update(valueBytes)
    }

    private fun MessageDigest.updateLength(value: Int) {
        update((value ushr 24).toByte())
        update((value ushr 16).toByte())
        update((value ushr 8).toByte())
        update(value.toByte())
    }

    private val currentNamespace = generate(currentContract())

    private const val CONTRACT_DOMAIN = "io.jankhunter.stable-symbol-contract.v1"
    private const val EMBEDDED_SYMBOL_FORMAT = 1
    private const val LOWERCASE_HEX = "0123456789abcdef"
    private const val SYMBOL_NAMESPACE_BYTES = 16
}
