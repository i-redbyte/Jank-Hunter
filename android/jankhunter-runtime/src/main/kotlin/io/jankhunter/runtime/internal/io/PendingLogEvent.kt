package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock

/**
 * Data envelope accepted by the asynchronous writer.
 *
 * Queue entries are typed so admission/loss accounting stays deterministic and queued work never
 * retains arbitrary closures. Pooled event implementations may reset their payload after write.
 */
internal sealed class PendingLogEvent(
    val recordType: Int,
    producerContext: LogEventContext?,
    captureProducer: Boolean = true,
) {
    private var producerContext: LogEventContext? = null
    private var producerElapsedUs = 0L
    private var producerThreadId = 0L

    init {
        if (captureProducer) captureProducer(producerContext)
    }

    /** Global admission order used to merge the independently bounded writer lanes. */
    var sequence: Long = 0L
        internal set

    open val logicalEventCount: Long = 1L

    open val remainingEventCount: Long
        get() = logicalEventCount

    internal open fun recycle() = Unit

    /** Discards an instance that was constructed but never became visible to the worker. */
    internal open fun rejectBeforeAdmission() = recycle()

    fun writeTo(writer: BinaryLogWriter) {
        val event = this
        writer.withProducer(producerElapsedUs, producerThreadId, producerContext) {
            event.writePayload(this)
        }
    }

    protected abstract fun writePayload(writer: BinaryLogWriter)

    protected fun captureProducer(context: LogEventContext?) {
        producerContext = context
        producerElapsedUs = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L
        producerThreadId = Process.myTid().toLong().coerceAtLeast(0L)
    }

    protected fun clearProducer() {
        producerContext = null
        producerElapsedUs = 0L
        producerThreadId = 0L
        sequence = 0L
    }
}
