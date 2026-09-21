package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.RuntimeLongSource

internal class MainThreadDispatchTracker(
    private val clockMs: RuntimeLongSource,
    private val minDurationMs: Long = 0L,
) {
    private var currentStartMs = 0L
    private var currentStartLine: String? = null

    fun onMessage(line: String): DispatchSample? {
        return when {
            line.startsWith(DISPATCH_START) -> {
                currentStartMs = clockMs.getAsLong()
                currentStartLine = line
                null
            }
            line.startsWith(DISPATCH_END) -> {
                val startLine = currentStartLine ?: return null
                val startMs = currentStartMs
                currentStartLine = null
                currentStartMs = 0L
                val durationMs = (clockMs.getAsLong() - startMs).coerceAtLeast(0L)
                if (durationMs < minDurationMs) return null
                DispatchSample(
                    durationMs = durationMs,
                    source = sourceFrom(startLine),
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

    data class DispatchSample(
        val durationMs: Long,
        val source: String,
    )

    companion object {
        private const val DISPATCH_START = ">>>>> Dispatching"
        private const val DISPATCH_END = "<<<<< Finished"
    }
}
