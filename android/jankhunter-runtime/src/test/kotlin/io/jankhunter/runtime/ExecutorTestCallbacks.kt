package io.jankhunter.runtime

/** Exercise the active adapter even when the test does not start a log writer. */
internal fun activeExecutorTestCallbacks(): RuntimeAsyncCallbacks = object : RuntimeAsyncCallbacks by JankHunter.asyncTelemetry() {
    override fun isExecutorActive(): Boolean = true
}
