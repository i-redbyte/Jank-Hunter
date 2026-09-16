package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.atomic.AtomicLong

/** Owns at most two consumers; a third requested generation waits without allocating producer state. */
internal class RuntimeHookEventTransport(
    private val maxCounterKeys: RuntimeIntSource,
    private val maxLogSpamKeys: RuntimeIntSource,
    private val exactAdmission: RuntimeBooleanSource = RuntimeBooleanSource { true },
    private val admissionWaitNanos: RuntimeLongSource = RuntimeLongSource { 5_000_000L },
    private val consumerDelayNanos: Long = 0L,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
    private val consumerThreadFactory: (Runnable, String) -> Thread = ::Thread,
    private val producerRegistrationObserver: (() -> Unit)? = null,
) {
    private val lifecycleLock = Any()
    @Volatile private var retired: RuntimeHookEventSession? = null
    @Volatile private var primary: RuntimeHookEventSession? = null
    @Volatile private var publication: Publication? = null
    @Volatile private var pending: Publication? = null

    fun start(writer: AsyncLogWriter) = synchronized(lifecycleLock) {
        check(primary?.isAccepting != true) { "Runtime hook event consumer is already running" }
        closePending()
        val request = Publication(writer)
        pending = request
        publication = request
        activatePending()
    }

    private fun activatePending() {
        val request = pending ?: return
        if (retired?.hasConsumerOwner != true) retired = null
        val previous = primary
        if (previous?.hasConsumerOwner == true) {
            if (retired != null) return
            retired = previous
        }
        lateinit var session: RuntimeHookEventSession
        session = RuntimeHookEventSession(
            maxCounterKeys, maxLogSpamKeys, exactAdmission, admissionWaitNanos, consumerDelayNanos,
            publisherAdmissionObserver, consumerLoopObserver, consumerThreadFactory, producerRegistrationObserver,
            onStopped = { onStopped(session) },
        )
        primary = session
        pending = null
        try {
            session.start(request.writer) {
                publication = Publication(request.writer, session)
                request.close()
            }
        } catch (error: Throwable) {
            publication = Publication(request.writer, session)
            request.close()
            throw error
        }
    }

    private fun onStopped(session: RuntimeHookEventSession) {
        // Publish pending before inspecting owners in start. A completion either sees that request,
        // or start itself observes the fully stopped owner. Ordinary completion needs no monitor.
        if (pending != null || retired === session) synchronized(lifecycleLock) {
            if (retired === session) retired = null
            activatePending()
        }
    }

    fun recordMethod(methodId: Long, methodName: String): Boolean {
        val target = publication ?: return false
        return target.session?.recordMethod(methodId, methodName) ?: target.reject()
    }

    fun recordLogSpam(screen: String?, owner: String?, source: String?, level: Int, operationId: Long = 0L): Boolean {
        val target = publication ?: return false
        return target.session?.recordLogSpam(screen, owner, source, level, operationId) ?: target.reject()
    }

    fun flushBlocking(timeoutMs: Long): Boolean {
        val target = publication
        if (target?.session != null) return target.session.flushBlocking(timeoutMs)
        if (target != null) {
            synchronized(lifecycleLock) { if (pending === target) target.flush() }
            return true
        }
        return primary?.flushBlocking(timeoutMs) ?: true
    }

    fun stopAndFlush(timeoutMs: Long): Boolean {
        val session = synchronized(lifecycleLock) {
            val active = if (pending == null) primary else null
            active?.requestStop()
            publication = null
            closePending()
            active
        }
        return session?.stopAndFlush(timeoutMs) ?: true
    }

    fun clear() = synchronized(lifecycleLock) {
        publication = null
        closePending()
        primary?.clear()
        retired?.clear()
    }

    private fun closePending() {
        pending?.close()
        pending = null
    }

    fun whenWriterDrained(writer: AsyncLogWriter, action: () -> Unit) {
        val session = synchronized(lifecycleLock) {
            primary?.takeIf { it.ownsWriter(writer) } ?: retired?.takeIf { it.ownsWriter(writer) }
        }
        if (session == null) action() else session.whenDrained(action)
    }

    internal fun acceptedForTest(): Long = primary?.acceptedForTest() ?: 0L
    internal fun emittedForTest(): Long = primary?.emittedForTest() ?: 0L
    internal fun acceptedLossForTest(): Long = primary?.acceptedLossForTest() ?: 0L
    internal fun backpressureCountForTest(): Long = primary?.backpressureCountForTest() ?: 0L
    internal fun attemptedForTest(): Long = primary?.attemptedForTest() ?: 0L
    internal fun consumerForTest(): Thread? = primary?.consumerForTest()
    internal fun acceptingPublishersForTest(): Boolean = publication?.session?.acceptingPublishersForTest() == true
    internal fun registeredProducerCountForTest(): Int = primary?.registeredProducerCountForTest() ?: 0

    private class Publication(val writer: AsyncLogWriter, val session: RuntimeHookEventSession? = null) {
        private val rejected = AtomicLong()

        fun reject(): Boolean {
            // getAndSet in close is the cancellation boundary. A stale publisher after it only
            // increments the negative closed value and cannot add to a sealed writer.
            rejected.getAndIncrement()
            return false
        }

        fun close() = report(rejected.getAndSet(Long.MIN_VALUE))
        fun flush() = report(rejected.getAndSet(0L))

        private fun report(count: Long) {
            if (count > 0L) writer.recordQuality(QualityCounterId.RUNTIME_EVENT_GENERATION_CAPACITY_LOSS, count)
        }
    }
}
