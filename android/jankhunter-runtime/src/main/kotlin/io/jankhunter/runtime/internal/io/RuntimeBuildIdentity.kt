package io.jankhunter.runtime.internal.io

import java.io.FileNotFoundException
import io.jankhunter.runtime.RuntimeHookGuard
import java.io.InputStream

/** No nullable digest that could be confused with an unminified or unreadable build. */
internal sealed interface RuntimeBuildIdentity {
    data class Mapped(val mappingSha256: String, val symbolNamespace: String) : RuntimeBuildIdentity
    data class Unminified(val symbolNamespace: String) : RuntimeBuildIdentity
    data class Unknown(val reason: Reason) : RuntimeBuildIdentity

    enum class Reason(val wireValue: Long) {
        MISSING(1L), UNREADABLE(2L), MALFORMED(3L), UNSUPPORTED_SCHEMA(4L), NAMESPACE_MISMATCH(5L),
    }
}

/** Process composition owns this cache. It retains identity strings, never Context or AssetManager. */
internal class RuntimeBuildIdentityResolver {
    @Volatile private var cached: RuntimeBuildIdentity? = null

    fun resolve(openAsset: () -> InputStream): RuntimeBuildIdentity {
        cached?.let { return it }
        return synchronized(this) {
            cached ?: read(openAsset).also { cached = it }
        }
    }

    private fun read(openAsset: () -> InputStream): RuntimeBuildIdentity = runCatching {
        openAsset().use(::parse)
    }.getOrElse { error ->
        RuntimeHookGuard.rethrowFatal(error)
        val reason = if (error is FileNotFoundException) RuntimeBuildIdentity.Reason.MISSING else RuntimeBuildIdentity.Reason.UNREADABLE
        RuntimeBuildIdentity.Unknown(reason)
    }

    private fun parse(input: InputStream): RuntimeBuildIdentity {
        val bytes = ByteArray(MAX_ASSET_BYTES + 1)
        var count = 0
        while (count < bytes.size) {
            val read = input.read(bytes, count, bytes.size - count)
            if (read < 0) break
            if (read == 0) return malformed()
            count += read
        }
        if (count > MAX_ASSET_BYTES || (0 until count).any { bytes[it].toInt() !in 0..127 }) return malformed()
        val text = String(bytes, 0, count, Charsets.US_ASCII)
        if (!text.endsWith('\n')) return malformed()
        val lines = text.removeSuffix("\n").split('\n')
        val fields = LinkedHashMap<String, String>()
        for (line in lines) {
            val separator = line.indexOf('=')
            if (separator < 1) return malformed()
            val key = line.substring(0, separator)
            if (fields.put(key, line.substring(separator + 1)) != null) return malformed()
        }
        if (fields.keys != setOf("schema", "state", "mapping-sha256", "symbol-namespace")) return malformed()
        if (fields["schema"] != "1") return RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.UNSUPPORTED_SCHEMA)
        val namespace = fields.getValue("symbol-namespace")
        if (!isLowerHex(namespace, 32)) return malformed()
        val digest = fields.getValue("mapping-sha256")
        return when (fields["state"]) {
            "mapped" -> if (isLowerHex(digest, 64)) RuntimeBuildIdentity.Mapped(digest, namespace) else malformed()
            "unminified" -> if (digest.isEmpty()) RuntimeBuildIdentity.Unminified(namespace) else malformed()
            else -> malformed()
        }
    }

    private fun malformed() = RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.MALFORMED)
    private fun isLowerHex(value: String, length: Int): Boolean = value.length == length && value.all { it in '0'..'9' || it in 'a'..'f' }

    companion object {
        const val ASSET_PATH = "jankhunter/build-identity-v1.txt"
        private const val MAX_ASSET_BYTES = 512
    }
}

internal fun RuntimeBuildIdentity.forNamespace(namespace: ByteArray): RuntimeBuildIdentity {
    val declared = when (this) {
        is RuntimeBuildIdentity.Mapped -> symbolNamespace
        is RuntimeBuildIdentity.Unminified -> symbolNamespace
        is RuntimeBuildIdentity.Unknown -> return this
    }
    val actual = buildString(namespace.size * 2) {
        for (byte in namespace) {
            val value = byte.toInt() and 0xff
            append("0123456789abcdef"[value ushr 4])
            append("0123456789abcdef"[value and 15])
        }
    }
    return if (declared == actual) this else RuntimeBuildIdentity.Unknown(RuntimeBuildIdentity.Reason.NAMESPACE_MISMATCH)
}

internal fun BinaryPayload.buildIdentity(identity: RuntimeBuildIdentity): BinaryPayload {
    when (identity) {
        is RuntimeBuildIdentity.Mapped -> {
            val text = identity.mappingSha256
            require(text.length == 64 && text.all { it in '0'..'9' || it in 'a'..'f' }) { "Invalid mapping SHA-256" }
            val digest = ByteArray(32) { index ->
                ((Character.digit(text[index * 2], 16) shl 4) or Character.digit(text[index * 2 + 1], 16)).toByte()
            }
            uvarint(2L).boundedBytes(digest, 32).uvarint(0L)
        }
        is RuntimeBuildIdentity.Unminified -> uvarint(1L).boundedBytes(ByteArray(0), 32).uvarint(0L)
        is RuntimeBuildIdentity.Unknown -> uvarint(0L).boundedBytes(ByteArray(0), 32).uvarint(identity.reason.wireValue)
    }
    return this
}
