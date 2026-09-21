package io.jankhunter.runtime.internal.system

internal fun interface NamedLongRecorder {
    fun record(name: String, value: Long)
}

internal fun interface DispatchSampleRecorder {
    fun record(durationMs: Long, thresholdMs: Long, source: String?)
}
