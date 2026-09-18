package io.jankhunter.gradle

/** Closed build-time states; absence and an unsupported OkHttp layout are distinct evidence. */
internal enum class TransportCapability(val encoded: String) {
    AVAILABLE("transport-v1:available\n"),
    MISSING_HELPER("transport-v1:missing-helper\n"),
    UNSUPPORTED_CODEC("transport-v1:unsupported-codec\n"),
    ;

    companion object {
        fun decode(text: String): TransportCapability = entries.firstOrNull { it.encoded == text }
            ?: error("Invalid transport capability record")
    }
}
