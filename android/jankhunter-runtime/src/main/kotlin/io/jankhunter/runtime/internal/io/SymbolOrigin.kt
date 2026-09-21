package io.jankhunter.runtime.internal.io

/** Wire V1 provenance; legacy producers are UNKNOWN regardless of stable ID or spelling. */
internal enum class SymbolOrigin(val wireValue: Int) {
    UNKNOWN(0), SOURCE_LABEL(1), RUNTIME_CLASS(2), RUNTIME_STACK(3),
}
