package io.jankhunter.gradle

/** Bytecode call sites use a positive, stable ID and retain the source text for missing catalogs. */
internal data class CausalSourceSite(
    val id: Long,
    val method: String,
    val line: Int,
    val origin: CausalCodeOrigin,
)

internal object CausalSourceSiteIds {
    fun forInvocation(
        className: String,
        methodName: String,
        descriptor: String,
        line: Int?,
        ordinal: Int,
        origin: CausalCodeOrigin,
    ): CausalSourceSite {
        val normalizedLine = line?.coerceAtLeast(0) ?: 0
        val canonical = "$className\u0000$methodName\u0000$descriptor\u0000$normalizedLine\u0000$ordinal"
        var hash = 0xcbf29ce484222325UL
        for (byte in canonical.encodeToByteArray()) {
            hash = (hash xor byte.toUByte().toULong()) * 0x100000001b3UL
        }
        val positive = (hash and Long.MAX_VALUE.toULong()).toLong().coerceAtLeast(1L)
        val method = "${className.replace('/', '.')}#$methodName$descriptor"
        return CausalSourceSite(positive, method, normalizedLine, origin)
    }
}
