package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.util.concurrent.atomic.AtomicLongArray
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/** Epoch ownership is kept by the runtime. Public operation tokens contain primitive IDs only. */
internal class RuntimeCollectionEpochs {
    private val ids = RuntimeAsyncTokenIds()
    private val lifecycle = ReentrantLock()
    private val pendingRejections = AtomicLongArray(RuntimeAsyncTokenTable.REJECT_ID_EXHAUSTED + 2)

    @Volatile
    var current: RuntimeCollectionEpoch? = null
        private set

    fun open(writer: AsyncLogWriter, config: JankHunterConfig) = lifecycle.withLock {
        check(current == null)
        val id = ids.next()
        if (id == 0L) {
            writer.recordQuality(QualityCounterId.ASYNC_TOKEN_ID_EXHAUSTED)
            return@withLock
        }
        val epoch = RuntimeCollectionEpoch(id, writer, config, RuntimeAsyncTokenTable(ids, rejected = ::reject))
        current = epoch
        flushPending(writer)
    }

    fun close(expectedWriter: AsyncLogWriter? = null) = lifecycle.withLock {
        val epoch = current ?: return@withLock
        if (expectedWriter != null && epoch.writer !== expectedWriter) return@withLock
        current = null
        epoch.tokens.close { kind, unfinished, completing ->
            epoch.writer.recordQuality(QualityCounterId.ASYNC_UNFINISHED_HTTP + kind - 1, unfinished)
            epoch.writer.recordQuality(QualityCounterId.ASYNC_COMPLETION_IN_PROGRESS_AT_STOP, completing)
        }
        flushPending(epoch.writer)
    }

    fun reject(reason: Int) {
        while (true) {
            val previous = pendingRejections.get(reason)
            if (previous == Long.MAX_VALUE || pendingRejections.compareAndSet(reason, previous, previous + 1L)) break
        }
        // A table may call this while close holds lifecycle and waits for that table. Never wait.
        // Counts which miss the closing snapshot remain pending for the next collection epoch.
        if (!lifecycle.tryLock()) return
        try {
            current?.writer?.takeIf { it.isAcceptingEvents() }?.let(::flushPending)
        } finally {
            lifecycle.unlock()
        }
    }

    private fun flushPending(writer: AsyncLogWriter) {
        for (reason in 1 until pendingRejections.length()) {
            writer.recordQuality(rejectionCounter(reason), pendingRejections.getAndSet(reason, 0L))
        }
    }

    private fun rejectionCounter(reason: Int): Int = when (reason) {
        RuntimeAsyncTokenTable.REJECT_CLOSED -> QualityCounterId.ASYNC_COMPLETION_STALE
        RuntimeAsyncTokenTable.REJECT_CONSUMED -> QualityCounterId.ASYNC_COMPLETION_DUPLICATE
        RuntimeAsyncTokenTable.REJECT_INVALID -> QualityCounterId.ASYNC_COMPLETION_INVALID
        RuntimeAsyncTokenTable.REJECT_CAPACITY -> QualityCounterId.ASYNC_TOKEN_CAPACITY_REJECTED
        RuntimeAsyncTokenTable.REJECT_ID_EXHAUSTED -> QualityCounterId.ASYNC_TOKEN_ID_EXHAUSTED
        else -> QualityCounterId.ASYNC_COMPLETION_FEATURE_DISABLED
    }

    companion object {
        const val REJECT_FEATURE_DISABLED = RuntimeAsyncTokenTable.REJECT_ID_EXHAUSTED + 1
    }
}

internal class RuntimeCollectionEpoch(
    val id: Long,
    val writer: AsyncLogWriter,
    val config: JankHunterConfig,
    val tokens: RuntimeAsyncTokenTable,
)
