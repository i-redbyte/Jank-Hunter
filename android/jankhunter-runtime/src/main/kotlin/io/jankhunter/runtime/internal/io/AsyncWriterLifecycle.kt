package io.jankhunter.runtime.internal.io

import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/** Owns the asynchronous writer state machine shared by producer, worker and shutdown threads. */
internal class AsyncWriterLifecycle {
    private val termination = AtomicReference<Termination?>()
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
        if (termination.get() != null) return
        termination.compareAndSet(null, Termination(reason, failure))
    }

    fun terminalReason(): Int = termination.get()?.reason ?: TERMINAL_REASON_NONE

    fun terminalFailure(): Throwable? = termination.get()?.failure

    fun markSessionFinished(): Boolean = sessionFinished.compareAndSet(false, true)

    fun deliverTerminalCallback(
        writer: AsyncLogWriter,
        callback: AsyncWriterTerminalObserver,
    ) {
        val terminal = termination.get() ?: return
        val reason = terminal.reason
        if (reason == TERMINAL_REASON_NONE || !terminalCallbackDelivered.compareAndSet(false, true)) return
        try {
            callback.onTerminal(writer, reason, terminal.failure)
        } catch (_: Throwable) {
            // A lifecycle callback must not escape the fail-open writer boundary.
        }
    }

    private class Termination(val reason: Int, val failure: Throwable?)

    private companion object {
        const val TERMINAL_REASON_NONE = 0
    }
}
