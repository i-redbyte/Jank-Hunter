package io.jankhunter.runtime

/**
 * Stable flags accepted by [JankHunter.recordHttp].
 *
 * The narrow contract lets optional network integrations describe an HTTP event without depending
 * on the binary log implementation.
 */
object JankHunterNetworkEventFlags {
    const val HTTP_REUSED_CONNECTION: Long = 1L
    const val HTTP_FAILED: Long = 1L shl 1
    const val HTTP_TLS: Long = 1L shl 2
    const val HTTP_SLOW: Long = 1L shl 15
    const val HTTP_CLASSIFIED: Long = 1L shl 17
}
