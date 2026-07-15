package io.jankhunter.runtime

import java.io.File

internal class RuntimeCoordinator(
    private val state: RuntimeState,
    private val nowMs: () -> Long,
) {
    fun isStopped(): Boolean = state.lifecycle == RuntimeLifecycle.STOPPED

    fun isStarting(): Boolean = state.lifecycle == RuntimeLifecycle.STARTING

    fun tryBeginStart(): Boolean {
        if (state.lifecycle != RuntimeLifecycle.STOPPED) return false
        state.started.set(false)
        state.lifecycle = RuntimeLifecycle.STARTING
        return true
    }

    fun markStarted() {
        state.lifecycle = RuntimeLifecycle.STARTED
        state.started.set(true)
    }

    fun beginStop(): Boolean {
        state.started.set(false)
        if (state.lifecycle == RuntimeLifecycle.STOPPED && state.writer == null) return false
        state.lifecycle = RuntimeLifecycle.STOPPING
        return true
    }

    fun markStopped() {
        state.started.set(false)
        state.lifecycle = RuntimeLifecycle.STOPPED
    }

    fun isActiveForHooks(): Boolean {
        return state.runtimeEnabled.get() &&
            state.started.get() &&
            state.writer?.isAcceptingEvents() == true
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
