package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.io.File

internal class RuntimeCoordinator(
    private val state: RuntimeState,
    private val nowMs: RuntimeLongSource,
) {
    fun isStopped(): Boolean = state.lifecycle == RuntimeLifecycle.STOPPED

    fun isStarting(): Boolean = state.lifecycle == RuntimeLifecycle.STARTING

    fun tryBeginStart(): Boolean {
        if (state.lifecycle != RuntimeLifecycle.STOPPED) return false
        disableHooks()
        state.started.set(false)
        state.lifecycle = RuntimeLifecycle.STARTING
        return true
    }

    fun markStarted(config: JankHunterConfig) {
        state.writer?.let { writer ->
            state.collectionEpochs.open(writer, config)
            writer.bindCollectionEndObserver {
                recordCollectionEnd(writer)
                state.collectionEpochs.close(writer)
            }
        }
        state.lifecycle = RuntimeLifecycle.STARTED
        state.started.set(true)
        state.featureGate.activate(config)
        state.writer?.recordQualityOnce(QualityCounterId.COLLECTION_WINDOW_START_ELAPSED_MS,
            nowMs.getAsLong().coerceIn(0L, Long.MAX_VALUE - 1L) + 1L)
    }

    fun beginStop(): Boolean {
        disableHooks()
        state.collectionEpochs.close()
        state.started.set(false)
        if (state.lifecycle == RuntimeLifecycle.STOPPED && state.writer == null) return false
        state.lifecycle = RuntimeLifecycle.STOPPING
        return true
    }

    fun markStopped() {
        disableHooks()
        state.collectionEpochs.close()
        state.started.set(false)
        state.lifecycle = RuntimeLifecycle.STOPPED
    }

    fun disableHooks() {
        state.writer?.let(::recordCollectionEnd)
        state.featureGate.deactivate()
    }

    private fun recordCollectionEnd(writer: AsyncLogWriter) {
        writer.recordQualityOnce(QualityCounterId.COLLECTION_WINDOW_END_ELAPSED_MS, nowMs.getAsLong())
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
            atMs = nowMs.getAsLong(),
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
            atMs = nowMs.getAsLong(),
            attempts = attempt,
            failures = failures,
        )
        state.initDiagnostics = diagnostics
    }
}
