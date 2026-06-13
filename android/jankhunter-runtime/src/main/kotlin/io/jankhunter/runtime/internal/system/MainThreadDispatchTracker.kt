package io.jankhunter.runtime.internal.system

internal class MainThreadDispatchTracker(
    private val clockMs: () -> Long,
    private val minDurationMs: Long = 0L,
) {
    private var current: DispatchStart? = null

    fun onMessage(line: String): DispatchSample? {
        return when {
            line.startsWith(DISPATCH_START) -> {
                current = DispatchStart(clockMs(), line)
                null
            }
            line.startsWith(DISPATCH_END) -> {
                val start = current ?: return null
                current = null
                val durationMs = (clockMs() - start.atMs).coerceAtLeast(0L)
                if (durationMs < minDurationMs) return null
                DispatchSample(
                    durationMs = durationMs,
                    source = sourceFrom(start.line),
                )
            }
            else -> null
        }
    }

    private fun sourceFrom(line: String): String {
        val marker = " to "
        val markerIndex = line.indexOf(marker)
        if (markerIndex < 0) return "unknown"
        val raw = line.substring(markerIndex + marker.length)
            .substringBefore("}")
            .substringBefore("{")
            .substringBefore(":")
            .trim()
        return raw.takeIf { it.isNotEmpty() } ?: "unknown"
    }

    private data class DispatchStart(
        val atMs: Long,
        val line: String,
    )

    data class DispatchSample(
        val durationMs: Long,
        val source: String,
    )

    companion object {
        private const val DISPATCH_START = ">>>>> Dispatching"
        private const val DISPATCH_END = "<<<<< Finished"
    }
}
