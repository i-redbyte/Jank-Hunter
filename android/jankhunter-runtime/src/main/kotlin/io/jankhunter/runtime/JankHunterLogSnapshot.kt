package io.jankhunter.runtime

import java.util.Collections

/**
 * Stable file snapshot captured while collection continues. Compatibility copies live in the app
 * cache until [close] or later cache eviction; copy them before retaining paths for deferred use.
 */
class JankHunterLogSnapshot internal constructor(
    val capturedAtMs: Long,
    logPaths: List<String>,
    val processCount: Int = 1,
    val captureSkewMs: Long = 0L,
    logByteLimits: List<Long> = emptyList(),
    private val release: (() -> Unit)? = null,
) : java.io.Closeable {
    override fun close() { release?.invoke() }
    val logPaths: List<String> = Collections.unmodifiableList(ArrayList(logPaths))
    internal val logByteLimits: List<Long> = Collections.unmodifiableList(ArrayList(logByteLimits))

    init {
        require(logByteLimits.isEmpty() || logByteLimits.size == logPaths.size)
        require(logByteLimits.all { it >= -1L })
    }

    internal fun byteLimit(index: Int): Long = logByteLimits.getOrNull(index)?.takeIf { it >= 0L }
        ?: java.io.File(logPaths[index]).length()
}
