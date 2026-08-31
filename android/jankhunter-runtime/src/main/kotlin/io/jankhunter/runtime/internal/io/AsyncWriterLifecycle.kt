package io.jankhunter.runtime.internal.io

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference

/** Owns the asynchronous writer state machine shared by producer, worker and shutdown threads. */
internal class AsyncWriterLifecycle {
    private val terminalReason = AtomicInteger(TERMINAL_REASON_NONE)
    private val terminalFailure = AtomicReference<Throwable?>()
    private val terminalCallbackDelivered = AtomicBoolean(false)
    private val sessionFinished = AtomicBoolean(false)

    @Volatile
    private var accepting = true

    @Volatile
    private var running = true

    fun isAccepting(): Boolean = accepting

    fun isRunning(): Boolean = running

    fun stopAccepting() {
        accepting = false
        running = false
    }

    fun recordTermination(reason: Int, failure: Throwable? = null) {
        terminalReason.compareAndSet(TERMINAL_REASON_NONE, reason)
        if (failure != null) terminalFailure.compareAndSet(null, failure)
    }

    fun terminalReason(): Int = terminalReason.get()

    fun terminalFailure(): Throwable? = terminalFailure.get()

    fun markSessionFinished(): Boolean = sessionFinished.compareAndSet(false, true)

    fun deliverTerminalCallback(
        writer: AsyncLogWriter,
        callback: AsyncWriterTerminalObserver,
    ) {
        val reason = terminalReason.get()
        if (reason == TERMINAL_REASON_NONE || !terminalCallbackDelivered.compareAndSet(false, true)) return
        try {
            callback.onTerminal(writer, reason, terminalFailure.get())
        } catch (_: Throwable) {
            // A lifecycle callback must not escape the fail-open writer boundary.
        }
    }

    private companion object {
        const val TERMINAL_REASON_NONE = 0
    }
}
