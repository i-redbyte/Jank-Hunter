package io.jankhunter.runtime

import java.io.File

internal class RuntimeCoordinator(
    private val state: RuntimeState,
    private val nowMs: () -> Long,
) {
    fun tryMarkStarted(): Boolean {
        return state.started.compareAndSet(false, true)
    }

    fun markStopped() {
        state.started.set(false)
    }

    fun isActiveForHooks(): Boolean {
        return state.runtimeEnabled.get() && state.started.get() && state.writer != null
    }

    fun recordInitStatus(
        status: String,
        attempt: Long,
        processName: String? = null,
        logDirectory: File? = null,
    ) {
        val diagnostics = JankHunterInitDiagnostics(
            status = status,
            processName = processName,
            logDirectory = logDirectory?.absolutePath,
            atMs = nowMs(),
            attempts = attempt,
            failures = state.initFailures.get(),
        )
        state.initDiagnostics = diagnostics
    }

    fun recordInitFailure(
        throwable: Throwable,
        attempt: Long,
        processName: String?,
        logDirectory: File?,
    ) {
        val failures = state.initFailures.incrementAndGet()
        val diagnostics = JankHunterInitDiagnostics(
            status = "failed",
            failureClass = throwable.javaClass.simpleName ?: throwable.javaClass.name,
            failureMessage = throwable.message,
            processName = processName,
            logDirectory = logDirectory?.absolutePath,
            atMs = nowMs(),
            attempts = attempt,
            failures = failures,
        )
        state.initDiagnostics = diagnostics
    }
}
