package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.internal.io.BinaryLogWriter

internal object UiWindowClassifier {
    fun flags(jankCount: Long, p95Ms: Long, problemP95ThresholdMs: Long): Long {
        var flags = BinaryLogWriter.FLAG_UI_CLASSIFIED
        if (jankCount > 0L || p95Ms >= problemP95ThresholdMs.coerceAtLeast(1L)) {
            flags = flags or BinaryLogWriter.FLAG_UI_PROBLEM
        }
        return flags
    }
}
