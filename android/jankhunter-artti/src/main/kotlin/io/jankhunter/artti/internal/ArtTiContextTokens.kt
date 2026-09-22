package io.jankhunter.artti.internal

/** FNV-1a context tokens shared by integration hooks and explicit diagnostics captures. */
internal object ArtTiContextTokens {
    fun token(screen: String?, owner: String?, flow: String?, step: String?): Long {
        var hash = FNV_OFFSET_BASIS
        fun add(value: String?) {
            if (value == null) {
                hash = (hash xor NULL_MARKER) * FNV_PRIME
            } else {
                value.forEach { character ->
                    hash = (hash xor character.code.toLong()) * FNV_PRIME
                }
            }
            hash = (hash xor FIELD_SEPARATOR) * FNV_PRIME
        }
        add(screen)
        add(owner)
        add(flow)
        add(step)
        return if (hash == 0L) 1L else hash
    }

    private const val FNV_OFFSET_BASIS = -3750763034362895579L
    private const val FNV_PRIME = 1099511628211L
    private const val NULL_MARKER = 0xffL
    private const val FIELD_SEPARATOR = 0xfeL
}
