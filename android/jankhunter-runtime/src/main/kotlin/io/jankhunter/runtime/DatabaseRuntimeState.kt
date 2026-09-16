package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.Jhlog
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReferenceArray

internal const val DATABASE_CAPTURE_INSERT_ROW_ID = 1
internal const val DATABASE_CAPTURE_AFFECTED_ROWS = 2

internal fun databaseResultCountBucket(value: Long, capture: Int): Int {
    if (capture == DATABASE_CAPTURE_INSERT_ROW_ID) {
        return if (value < 0L) Jhlog.DATABASE_COUNT_ZERO.toInt() else Jhlog.DATABASE_COUNT_ONE.toInt()
    }
    if (capture != DATABASE_CAPTURE_AFFECTED_ROWS) return Jhlog.DATABASE_COUNT_UNKNOWN.toInt()
    return when {
        value <= 0L -> Jhlog.DATABASE_COUNT_ZERO.toInt()
        value == 1L -> Jhlog.DATABASE_COUNT_ONE.toInt()
        value <= 10L -> Jhlog.DATABASE_COUNT_TWO_TO_TEN.toInt()
        value <= 100L -> Jhlog.DATABASE_COUNT_ELEVEN_TO_HUNDRED.toInt()
        else -> Jhlog.DATABASE_COUNT_OVER_HUNDRED.toInt()
    }
}

internal class PreparedStatementSnapshot(
    statement: Any,
    val query: String?,
    val fingerprint: Long,
    val token: Long,
) {
    private val statement = WeakReference(statement)

    fun refersTo(candidate: Any): Boolean = statement.get() === candidate

    fun isRetained(): Boolean = statement.get() != null
}

internal class PreparedStatementRegistry(
    capacity: Int = DEFAULT_PREPARED_STATEMENT_CAPACITY,
    private val onEviction: () -> Unit = {},
    private val onResolutionMissAfterEviction: () -> Unit = {},
) {
    private val mask: Int
    private val snapshots: AtomicReferenceArray<PreparedStatementSnapshot?>
    private var nextToken = 1L
    private var evictionCursor = 0
    private val evictionCount = AtomicLong()

    init {
        require(capacity >= 2 && capacity and (capacity - 1) == 0)
        mask = capacity - 1
        snapshots = AtomicReferenceArray(capacity)
    }

    @Synchronized
    fun register(statement: Any, query: String?, fingerprint: Long): Long {
        val start = identityIndex(statement)
        var reclaim = -1
        val probes = minOf(snapshots.length(), MAX_PREPARED_STATEMENT_PROBES)
        for (offset in 0 until probes) {
            val index = (start + offset) and mask
            val snapshot = snapshots.get(index)
            if (snapshot?.refersTo(statement) == true) {
                return replace(index, statement, query, fingerprint, snapshot.token)
            }
            if (snapshot?.isRetained() != true && reclaim < 0) reclaim = index
        }
        val index = if (reclaim >= 0) {
            reclaim
        } else {
            if (evictionCount.get() != Long.MAX_VALUE) evictionCount.incrementAndGet()
            onEviction()
            (start + (evictionCursor++ and (probes - 1))) and mask
        }
        return replace(index, statement, query, fingerprint)
    }

    fun resolve(statement: Any?): PreparedStatementSnapshot? {
        if (statement == null) return null
        val start = identityIndex(statement)
        val probes = minOf(snapshots.length(), MAX_PREPARED_STATEMENT_PROBES)
        for (offset in 0 until probes) {
            val index = (start + offset) and mask
            val snapshot = snapshots.get(index)
            if (snapshot?.refersTo(statement) == true) return snapshot
        }
        if (evictionCount.get() != 0L) onResolutionMissAfterEviction()
        return null
    }

    @Synchronized
    internal fun retainedEntryCount(): Int {
        var count = 0
        for (index in 0 until snapshots.length()) {
            if (snapshots.get(index)?.isRetained() == true) {
                count++
            } else {
                snapshots.set(index, null)
            }
        }
        return count
    }

    internal fun evictionCount(): Long = evictionCount.get()

    internal fun capacityForTest(): Int = snapshots.length()

    private fun replace(
        index: Int,
        statement: Any,
        query: String?,
        fingerprint: Long,
        token: Long = nextToken(),
    ): Long {
        snapshots.set(index, PreparedStatementSnapshot(statement, query, fingerprint, token))
        return token
    }

    private fun nextToken(): Long {
        val token = nextToken
        nextToken = if (token == Long.MAX_VALUE) 1L else token + 1L
        return token
    }

    private fun identityIndex(statement: Any): Int {
        val hash = System.identityHashCode(statement)
        return (hash xor (hash ushr 16)) and mask
    }

    private companion object {
        // One AtomicReferenceArray replaces the former reference+snapshot arrays. 4K entries cost
        // only ~16 KiB more fixed memory while sharply reducing live-entry collisions.
        const val DEFAULT_PREPARED_STATEMENT_CAPACITY = 4_096
        const val MAX_PREPARED_STATEMENT_PROBES = 16
    }
}

internal class DatabaseTransactionTracker(
    private val ids: DatabaseTransactionIdGenerator = DatabaseTransactionIdGenerator(),
    private val completionSink: DatabaseTransactionCompletionSink = DatabaseTransactionCompletionSink.NONE,
    private val epochId: RuntimeLongSource = RuntimeLongSource { 0L },
    private val nanoTime: RuntimeLongSource,
) {
    private val localSlot = ThreadLocal<Array<Any?>>()
    private val statePool = TransactionStatePool()

    fun begin(
        database: Any,
        sourceId: Long,
        sourceName: String,
        mode: Long,
        rootParentId: Long = 0L,
        transactionId: Long = ids.next(),
        expectedEpochId: Long = epochId.getAsLong(),
    ): Long {
        val slot = currentSlotOrCreate(expectedEpochId) ?: return 0L
        var state = slot.active ?: statePool.acquire().also { slot.active = it }
        if (state.depth == state.capacity) state = state.grow().also { slot.active = it }
        val index = state.depth
        val id = transactionId
        state.receivers[index] = slot.receiver(database)
        state.ids[index] = id
        state.parents[index] = if (index == 0) rootParentId else state.ids[index - 1]
        state.sourceIds[index] = sourceId
        state.sourceNames[index] = sourceName
        state.modes[index] = mode
        state.startedNanos[index] = nanoTime.getAsLong().coerceAtLeast(0L)
        state.flags[index] = 0
        state.statementCounts[index] = 0L
        state.readCounts[index] = 0L
        state.writeCounts[index] = 0L
        state.depth = index + 1
        return id
    }

    fun markSuccessful(database: Any): Boolean {
        val state = currentSlot()?.active ?: return false
        val index = state.find(database)
        if (index < 0) return false
        state.flags[index] = (state.flags[index].toInt() or TRANSACTION_MARKED_SUCCESSFUL).toByte()
        return true
    }

    fun recordStatement(operation: Long, expectedEpochId: Long = epochId.getAsLong()): Long {
        val state = currentSlot(expectedEpochId)?.active ?: return 0L
        val index = state.depth - 1
        if (index < 0) return 0L
        state.statementCounts[index]++
        if (operation == Jhlog.DATABASE_OPERATION_QUERY) {
            state.readCounts[index]++
        } else {
            state.writeCounts[index]++
        }
        return state.ids[index]
    }

    fun finish(database: Any, failureKind: DatabaseFailureKind, failed: Boolean): Boolean {
        val slot = currentSlot() ?: return false
        val state = slot.active ?: return false
        val index = state.find(database)
        if (index < 0) return false
        val transactionId = state.ids[index]
        val parentId = state.parents[index]
        val sourceId = state.sourceIds[index]
        val sourceName = checkNotNull(state.sourceNames[index])
        val mode = state.modes[index]
        val outcome = when {
            failed -> Jhlog.DATABASE_TRANSACTION_FAILURE
            state.flags[index].toInt() == TRANSACTION_MARKED_SUCCESSFUL -> Jhlog.DATABASE_TRANSACTION_SUCCESS
            else -> Jhlog.DATABASE_TRANSACTION_ROLLBACK
        }
        val recordedFailureKind = if (failed) failureKind.wireValue else Jhlog.DATABASE_FAILURE_NONE
        val durationNanos = (nanoTime.getAsLong() - state.startedNanos[index]).coerceAtLeast(0L)
        val statementCount = state.statementCounts[index]
        val readCount = state.readCounts[index]
        val writeCount = state.writeCounts[index]
        if (outcome != Jhlog.DATABASE_TRANSACTION_SUCCESS) state.markParentFailed(database, index)
        state.remove(index)
        if (state.depth == 0) {
            slot.active = null
            statePool.release(state)
            localSlot.get()?.set(ACTIVE_SLOT_INDEX, null)
        }
        completionSink.complete(
            transactionId,
            parentId,
            sourceId,
            sourceName,
            mode,
            outcome,
            recordedFailureKind,
            durationNanos,
            statementCount,
            readCount,
            writeCount,
        )
        return true
    }

    fun currentTransactionId(expectedEpochId: Long = epochId.getAsLong()): Long {
        val state = currentSlot(expectedEpochId)?.active ?: return 0L
        return if (state.depth == 0) 0L else state.ids[state.depth - 1]
    }

    fun currentParentId(expectedEpochId: Long = epochId.getAsLong()): Long {
        val state = currentSlot(expectedEpochId)?.active ?: return 0L
        return if (state.depth == 0) 0L else state.parents[state.depth - 1]
    }

    private fun currentSlotOrCreate(expectedEpochId: Long): TransactionStateSlot? {
        if (epochId.getAsLong() != expectedEpochId) return null
        currentSlot(expectedEpochId)
        var holder = localSlot.get()
        if (holder == null) {
            holder = arrayOfNulls(SLOT_HOLDER_SIZE)
            localSlot.set(holder)
        }
        val active = holder[ACTIVE_SLOT_INDEX] as? TransactionStateSlot
        if (active != null) return active
        val cached = (holder[WEAK_SLOT_INDEX] as? WeakReference<*>)?.get() as? TransactionStateSlot
        val slot = cached ?: TransactionStateSlot().also { holder[WEAK_SLOT_INDEX] = WeakReference(it) }
        slot.epochId = expectedEpochId
        holder[ACTIVE_SLOT_INDEX] = slot
        return slot
    }

    private fun currentSlot(expectedEpochId: Long = epochId.getAsLong()): TransactionStateSlot? {
        if (epochId.getAsLong() != expectedEpochId) return null
        val holder = localSlot.get() ?: return null
        val slot = holder[ACTIVE_SLOT_INDEX] as? TransactionStateSlot ?: return null
        if (slot.epochId == expectedEpochId) return slot
        slot.active = null
        holder[ACTIVE_SLOT_INDEX] = null
        return null
    }

    private class TransactionStateSlot(var active: TransactionState? = null) {
        var epochId = 0L
        private val receiverCache = arrayOfNulls<WeakReference<Any>>(RECEIVER_CACHE_CAPACITY)
        private var replacementCursor = 0

        fun receiver(database: Any): WeakReference<Any> {
            var reclaim = -1
            for (index in receiverCache.indices) {
                val reference = receiverCache[index]
                val current = reference?.get()
                if (current === database) return reference
                if (current == null && reclaim < 0) reclaim = index
            }
            val index = if (reclaim >= 0) reclaim else replacementCursor++ and RECEIVER_CACHE_MASK
            return WeakReference(database).also { receiverCache[index] = it }
        }
    }

    private class TransactionStatePool {
        private val slots = AtomicReferenceArray<TransactionState?>(STATE_POOL_CAPACITY)
        private val cursor = AtomicInteger()

        fun acquire(): TransactionState {
            val start = cursor.getAndIncrement() and STATE_POOL_MASK
            repeat(STATE_POOL_CAPACITY) { offset ->
                slots.getAndSet((start + offset) and STATE_POOL_MASK, null)?.let { return it }
            }
            return TransactionState()
        }

        // A rare deep transaction must not permanently enlarge the cross-thread state pool.
        fun release(state: TransactionState) {
            if (state.capacity != INITIAL_TRANSACTION_DEPTH) return
            val start = cursor.getAndIncrement() and STATE_POOL_MASK
            repeat(STATE_POOL_CAPACITY) { offset ->
                if (slots.compareAndSet((start + offset) and STATE_POOL_MASK, null, state)) return
            }
        }
    }

    private class TransactionState(val capacity: Int = INITIAL_TRANSACTION_DEPTH) {
        var depth = 0
        val receivers: Array<WeakReference<Any>?> = arrayOfNulls(capacity)
        val ids = LongArray(capacity)
        val parents = LongArray(capacity)
        val sourceIds = LongArray(capacity)
        val sourceNames = arrayOfNulls<String>(capacity)
        val modes = LongArray(capacity)
        val startedNanos = LongArray(capacity)
        // Independent bits preserve child rollback at every depth and after out-of-order removal.
        val flags = ByteArray(capacity)
        val statementCounts = LongArray(capacity)
        val readCounts = LongArray(capacity)
        val writeCounts = LongArray(capacity)

        fun grow(): TransactionState {
            val nextCapacity = if (capacity <= Int.MAX_VALUE / 2) capacity * 2 else Int.MAX_VALUE
            check(nextCapacity > capacity) { "Transaction stack capacity exhausted" }
            return TransactionState(nextCapacity).also { next ->
                // Publish the new storage only after every array has been allocated and copied.
                receivers.copyInto(next.receivers, endIndex = depth)
                ids.copyInto(next.ids, endIndex = depth)
                parents.copyInto(next.parents, endIndex = depth)
                sourceIds.copyInto(next.sourceIds, endIndex = depth)
                sourceNames.copyInto(next.sourceNames, endIndex = depth)
                modes.copyInto(next.modes, endIndex = depth)
                startedNanos.copyInto(next.startedNanos, endIndex = depth)
                flags.copyInto(next.flags, endIndex = depth)
                statementCounts.copyInto(next.statementCounts, endIndex = depth)
                readCounts.copyInto(next.readCounts, endIndex = depth)
                writeCounts.copyInto(next.writeCounts, endIndex = depth)
                next.depth = depth
            }
        }

        fun find(database: Any): Int {
            for (index in depth - 1 downTo 0) {
                if (receivers[index]?.get() === database) return index
            }
            return -1
        }

        fun markParentFailed(database: Any, child: Int) {
            for (index in child - 1 downTo 0) {
                if (receivers[index]?.get() === database) {
                    flags[index] = (flags[index].toInt() or TRANSACTION_CHILD_FAILED).toByte()
                    return
                }
            }
        }

        fun remove(index: Int) {
            // Transactions on independent databases may finish out of global stack order.
            for (target in index until depth - 1) copy(target + 1, target)
            depth--
            receivers[depth] = null
            sourceNames[depth] = null
            flags[depth] = 0
            ids[depth] = 0L
            parents[depth] = 0L
            sourceIds[depth] = 0L
            modes[depth] = 0L
            startedNanos[depth] = 0L
            statementCounts[depth] = 0L
            readCounts[depth] = 0L
            writeCounts[depth] = 0L
        }

        private fun copy(source: Int, target: Int) {
            receivers[target] = receivers[source]
            ids[target] = ids[source]
            parents[target] = parents[source]
            sourceIds[target] = sourceIds[source]
            sourceNames[target] = sourceNames[source]
            modes[target] = modes[source]
            startedNanos[target] = startedNanos[source]
            flags[target] = flags[source]
            statementCounts[target] = statementCounts[source]
            readCounts[target] = readCounts[source]
            writeCounts[target] = writeCounts[source]
        }
    }

    private companion object {
        const val INITIAL_TRANSACTION_DEPTH = 16
        const val TRANSACTION_MARKED_SUCCESSFUL = 1
        const val TRANSACTION_CHILD_FAILED = 2
        const val STATE_POOL_CAPACITY = 32
        const val STATE_POOL_MASK = STATE_POOL_CAPACITY - 1
        const val RECEIVER_CACHE_CAPACITY = 4
        const val RECEIVER_CACHE_MASK = RECEIVER_CACHE_CAPACITY - 1
        const val WEAK_SLOT_INDEX = 0
        const val ACTIVE_SLOT_INDEX = 1
        const val SLOT_HOLDER_SIZE = 2
    }
}

internal fun interface DatabaseTransactionCompletionSink {
    fun complete(
        transactionId: Long,
        parentId: Long,
        sourceId: Long,
        sourceName: String,
        mode: Long,
        outcome: Long,
        failureKind: Long,
        durationNanos: Long,
        statementCount: Long,
        readCount: Long,
        writeCount: Long,
    )

    companion object {
        val NONE = DatabaseTransactionCompletionSink { _, _, _, _, _, _, _, _, _, _, _ -> }
    }
}

internal class DatabaseTransactionIdGenerator {
    private val nextId = AtomicLong(1L)

    fun next(): Long {
        while (true) {
            val value = nextId.getAndIncrement()
            if (value > 0L) return value
            nextId.compareAndSet(value + 1L, 1L)
        }
    }
}

internal class ManualDatabaseTransactionTracker(
    private val ids: DatabaseTransactionIdGenerator,
    private val nanoTime: RuntimeLongSource,
    private val epochId: RuntimeLongSource = RuntimeLongSource { 0L },
) {
    private val current = ThreadLocal<WeakReference<JankHunterDatabaseTransactionToken>?>()

    fun begin(
        sourceId: Long,
        sourceName: String,
        mode: Long,
        automaticParentId: Long,
        transactionId: Long = ids.next(),
        expectedEpochId: Long = epochId.getAsLong(),
    ): JankHunterDatabaseTransactionToken {
        val manualParent = activeToken(expectedEpochId)
        val parentId = if (manualParent == null || automaticParentId > manualParent.id) {
            automaticParentId
        } else {
            manualParent.id
        }
        val token = JankHunterDatabaseTransactionToken(
            id = transactionId,
            sourceId = sourceId,
            sourceName = sourceName,
            mode = mode,
            parentId = parentId,
            startedNanos = nanoTime.getAsLong().coerceAtLeast(0L),
            parent = manualParent?.let(::WeakReference),
            collectionEpochId = expectedEpochId,
        )
        current.set(WeakReference(token))
        return token
    }

    fun currentTransactionId(expectedEpochId: Long = epochId.getAsLong()): Long = activeToken(expectedEpochId)?.id ?: 0L

    fun recordStatement(operation: Long, expectedEpochId: Long = epochId.getAsLong()): Long {
        val token = activeToken(expectedEpochId) ?: return 0L
        token.recordStatement(operation)
        return token.id
    }

    fun release(token: JankHunterDatabaseTransactionToken) {
        if (current.get()?.get() !== token) return
        val parent = token.parent?.get()?.takeUnless { it.isCompleted() }
        if (parent == null) {
            current.remove()
        } else {
            current.set(WeakReference(parent))
        }
    }

    private fun activeToken(expectedEpochId: Long = epochId.getAsLong()): JankHunterDatabaseTransactionToken? {
        if (epochId.getAsLong() != expectedEpochId) return null
        var token = current.get()?.get()
        if (token != null && token.collectionEpochId != expectedEpochId) token = null
        while (token != null && token.isCompleted()) token = token.parent?.get()
        if (token == null) {
            current.remove()
        } else if (current.get()?.get() !== token) {
            current.set(WeakReference(token))
        }
        return token
    }
}
