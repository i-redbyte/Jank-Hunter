package io.jankhunter.runtime.internal.system

internal class MainThreadDispatchTracker(
    private val clockMs: () -> Long,
) {
    private var current: DispatchStart? = null

    fun onMessage(line: String): DispatchSample? {
        return when {
            line.startsWith(DISPATCH_START) -> {
                current = DispatchStart(clockMs(), sourceFrom(line))
                null
            }
            line.startsWith(DISPATCH_END) -> {
                val start = current ?: return null
                current = null
                DispatchSample(
                    durationMs = (clockMs() - start.atMs).coerceAtLeast(0L),
                    source = start.source,
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
        val source: String,
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
