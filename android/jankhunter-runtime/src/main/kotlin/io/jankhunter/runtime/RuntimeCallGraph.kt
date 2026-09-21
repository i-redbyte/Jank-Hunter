package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.atomic.AtomicLong

/** Owns at most two consumers; a third requested generation waits without allocating producer state. */
internal class RuntimeCallGraph(
    private val nowMs: RuntimeLongSource,
    private val captureScreen: () -> String?,
    private val captureOperationId: RuntimeLongSource,
    private val maxKeys: RuntimeIntSource,
    private val exactAdmission: RuntimeBooleanSource = RuntimeBooleanSource { true },
    private val admissionWaitNanos: RuntimeLongSource = RuntimeLongSource { 5_000_000L },
    private val periodicFlushIntervalMs: Long = 30_000L,
    private val consumerDelayNanos: Long = 0L,
    private val batchObserver: ((RuntimeCallBatch) -> Unit)? = null,
    private val publisherAdmissionObserver: (() -> Unit)? = null,
    private val consumerLoopObserver: (() -> Unit)? = null,
    private val consumerThreadFactory: (Runnable, String) -> Thread = ::Thread,
    private val producerRegistrationObserver: (() -> Unit)? = null,
    private val uptimeNanos: RuntimeLongSource = RuntimeLongSource(System::nanoTime),
    private val producerStorageLimitBytes: Long = RuntimeGraphStorageBudget.DEFAULT_LIMIT_BYTES,
) {
    private val lifecycleLock = Any()
    private val epoch = AtomicLong(1L)
    private val skippedTokenSequence = AtomicLong()
    @Volatile private var retired: RuntimeCallGraphSession? = null
    @Volatile private var primary: RuntimeCallGraphSession? = null
    @Volatile private var publication: Publication? = null
    @Volatile private var pending: Publication? = null

    fun resetFlushState(writer: AsyncLogWriter) = synchronized(lifecycleLock) {
        check(primary?.isAccepting != true) { "Runtime graph consumer is already running" }
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
        advanceRuntimeEpoch(epoch)
        lateinit var session: RuntimeCallGraphSession
        session = RuntimeCallGraphSession(
            nowMs, captureScreen, captureOperationId, maxKeys, exactAdmission, admissionWaitNanos,
            periodicFlushIntervalMs, consumerDelayNanos, batchObserver, publisherAdmissionObserver,
            consumerLoopObserver, consumerThreadFactory, producerRegistrationObserver, uptimeNanos,
            generation = epoch.get(),
            onStopped = { onStopped(session) },
            storageLimitBytes = producerStorageLimitBytes,
            nextSkippedToken = RuntimeLongSource(::nextSkippedToken),
        )
        primary = session
        pending = null
        try {
            session.resetFlushState(request.writer) {
                publication = Publication(request.writer, session)
                request.close()
            }
        } catch (error: Throwable) {
            publication = Publication(request.writer, session)
            request.close()
            throw error
        }
    }

    // Negative tokens identify a single omitted stack interval across all generations. Reusing
    // the epoch would let a late exit from an old interval consume a new interval after a reset.
    private fun nextSkippedToken(): Long {
        repeat(32) {
            val previous = skippedTokenSequence.get()
            if (previous == Long.MIN_VALUE) return 0L
            if (skippedTokenSequence.compareAndSet(previous, previous - 1L)) return previous - 1L
        }
        return 0L
    }

    private fun onStopped(session: RuntimeCallGraphSession) {
        // Publish pending before inspecting owners in start. A completion either sees that request,
        // or start itself observes the fully stopped owner. Ordinary completion needs no monitor.
        if (pending != null || retired === session) synchronized(lifecycleLock) {
            if (retired === session) retired = null
            activatePending()
        }
    }

    fun enter(methodId: Long, methodName: String, enabled: Boolean): Long {
        if (!enabled) return 0L
        val target = publication ?: return 0L
        val session = target.session
        if (session != null) return session.enter(methodId, methodName, enabled)
        target.skipEntry()
        return 0L
    }

    fun exit(token: Long, methodId: Long) { publication?.session?.exit(token, methodId) }
    fun hasCurrentMethod(): Boolean = publication?.session?.hasCurrentMethod() == true
    fun currentMethodId(): Long = publication?.session?.currentMethodId() ?: 0L
    fun currentMethodName(): String? = publication?.session?.currentMethodName()

    fun recordSemantic(
        callerId: Long, callerName: String, calleeId: Long, calleeName: String, durationMs: Long, enabled: Boolean,
        expectedWriter: AsyncLogWriter? = null,
    ) {
        if (!enabled) return
        val target = publication ?: return
        if (expectedWriter != null && target.writer !== expectedWriter) return
        val session = target.session
        if (session != null) session.recordSemantic(callerId, callerName, calleeId, calleeName, durationMs, enabled)
        else target.reject()
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

    fun flushForShutdown(timeoutMs: Long = 2_000L): Boolean {
        val session = synchronized(lifecycleLock) {
            val active = if (pending == null) primary else null
            active?.requestStop()
            publication = null
            closePending()
            active
        }
        return session?.flushForShutdown(timeoutMs) ?: true
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

    internal fun aggregatedEdgeKeysForTest(): Long = primary?.aggregatedEdgeKeysForTest() ?: 0L
    internal fun currentThreadDepthForTest(): Int = primary?.currentThreadDepthForTest() ?: 0
    internal fun producerCapacityLossForTest(): Long = primary?.producerCapacityLossForTest() ?: 0L

    fun whenWriterDrained(writer: AsyncLogWriter, action: () -> Unit) {
        val session = synchronized(lifecycleLock) {
            primary?.takeIf { it.ownsWriter(writer) } ?: retired?.takeIf { it.ownsWriter(writer) }
        }
        if (session == null) action() else session.whenDrained(action)
    }

    internal fun acceptedForTest(): Long = primary?.acceptedForTest() ?: 0L
    internal fun emittedForTest(): Long = primary?.emittedForTest() ?: 0L
    internal fun acceptedEventLossForTest(): Long = primary?.acceptedEventLossForTest() ?: 0L
    internal fun backpressureCountForTest(): Long = primary?.backpressureCountForTest() ?: 0L
    internal fun attemptedForTest(): Long = primary?.attemptedForTest() ?: 0L
    internal fun consumerForTest(): Thread? = primary?.consumerForTest()
    internal fun acceptingPublishersForTest(): Boolean = publication?.session?.acceptingPublishersForTest() == true
    internal fun registeredProducerCountForTest(): Int = primary?.registeredProducerCountForTest() ?: 0
    internal fun storageUsedForTest(): Long = primary?.storageUsedForTest() ?: 0L
    internal fun storagePeakForTest(): Long = primary?.storagePeakForTest() ?: 0L

    private class Publication(val writer: AsyncLogWriter, val session: RuntimeCallGraphSession? = null) {
        private val rejected = AtomicLong()
        private val skippedEntries = AtomicLong()

        fun skipEntry() { skippedEntries.getAndIncrement() }

        fun reject(): Boolean {
            // getAndSet in close is the cancellation boundary. A stale publisher after it only
            // increments the negative closed value and cannot add to a sealed writer.
            rejected.getAndIncrement()
            return false
        }

        fun close() {
            report(rejected.getAndSet(Long.MIN_VALUE))
            reportEntries(skippedEntries.getAndSet(Long.MIN_VALUE))
        }

        fun flush() {
            report(rejected.getAndSet(0L))
            reportEntries(skippedEntries.getAndSet(0L))
        }

        private fun reportEntries(count: Long) {
            if (count > 0L) writer.recordQuality(QualityCounterId.RUNTIME_GRAPH_GENERATION_SKIPPED_ENTRY_TOTAL, count)
        }

        private fun report(count: Long) {
            if (count > 0L) {
                writer.recordQuality(QualityCounterId.RUNTIME_GRAPH_GENERATION_CAPACITY_LOSS, count)
                writer.recordQuality(QualityCounterId.RUNTIME_GRAPH_INPUT_TOTAL, count)
            }
        }
    }
}
