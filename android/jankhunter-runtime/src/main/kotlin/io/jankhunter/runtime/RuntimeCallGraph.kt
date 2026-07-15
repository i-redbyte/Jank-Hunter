package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong

/**
 * Allocation-free per-thread call stack plus a bounded, primitive edge table.
 *
 * Method IDs are opaque unsigned 64-bit values carried in a Kotlin [Long]. Every bit pattern,
 * including zero and negative values, is valid. The token returned by [enter] is therefore a
 * separate non-zero epoch and never encodes a method ID or a timestamp.
 */
internal class RuntimeCallGraph(
    private val nowMs: () -> Long,
    private val captureContext: () -> JankHunterContext,
    private val maxKeys: () -> Int,
) {
    private val stack = ThreadLocal<RuntimeCallStack>()
    private val shards = Array(SHARD_COUNT) { EdgeShard() }
    private val entryCount = AtomicInteger()
    private val epoch = AtomicLong(INITIAL_EPOCH)
    private val capacityLoss = AtomicLong()
    private val stackMismatch = AtomicLong()
    private val lastFlushAtMs = AtomicLong()
    private val flushLock = Any()
    private var nextFlushShard = 0

    fun resetFlushState() {
        advanceEpoch()
        lastFlushAtMs.set(nowMs())
        capacityLoss.set(0L)
        stackMismatch.set(0L)
    }

    fun clear() {
        advanceEpoch()
        shards.forEach { shard ->
            synchronized(shard.lock) {
                shard.clear()
            }
        }
        entryCount.set(0)
        stack.remove()
        lastFlushAtMs.set(0L)
        capacityLoss.set(0L)
        stackMismatch.set(0L)
    }

    fun enter(methodId: Long, enabled: Boolean): Long = enter(methodId, null, enabled)

    fun enter(methodId: Long, methodName: String?, enabled: Boolean): Long {
        if (!enabled) return DISABLED_TOKEN
        val currentEpoch = epoch.get()
        val now = nowMs()
        val currentStack = stack.get() ?: RuntimeCallStack(currentEpoch).also(stack::set)
        if (currentStack.epoch != currentEpoch) {
            currentStack.reset(currentEpoch)
        }
        currentStack.push(methodId, methodName, now)
        return currentEpoch
    }

    /** Pops the stack even when [writer] is absent so a transient writer failure cannot poison it. */
    fun exit(token: Long, methodId: Long, writer: AsyncLogWriter?) {
        if (token == DISABLED_TOKEN) return
        val currentEpoch = epoch.get()
        val currentStack = stack.get()
        if (currentStack == null) {
            if (token == currentEpoch) stackMismatch.incrementAndGet()
            return
        }
        if (currentStack.epoch != currentEpoch) {
            currentStack.reset(currentEpoch)
        }
        if (token != currentEpoch) return

        if (!currentStack.pop(methodId)) {
            stackMismatch.incrementAndGet()
            if (currentStack.depth == 0) stack.remove()
            return
        }
        if (currentStack.depth == 0) stack.remove()

        // The stack mutation above is deliberately independent of writer availability.
        if (!currentStack.hasPoppedParent || writer == null) return
        val now = nowMs()
        val durationMs = (now - currentStack.poppedStartedAtMs).coerceAtLeast(0L)
        recordEdge(
            currentStack.poppedParentId,
            currentStack.poppedParentName,
            methodId,
            currentStack.poppedName,
            durationMs,
            currentEpoch,
        )
        maybeFlush(now, writer)
    }

    /** Emits at most one bounded batch. Repeated/manual flushes can drain subsequent batches. */
    fun flush(force: Boolean, writer: AsyncLogWriter?) {
        val asyncWriter = writer ?: return
        flushAt(nowMs(), force, asyncWriter)
    }

    /** Drains through bounded queue items; the default 4096-key table needs at most 32 items. */
    fun flushForShutdown(writer: AsyncLogWriter?) {
        advanceEpoch()
        val asyncWriter = writer ?: return
        synchronized(flushLock) {
            while (true) {
                val batch = takeBatch() ?: break
                asyncWriter.runtimeCalls(batch)
            }
            flushQuality(asyncWriter)
            lastFlushAtMs.set(nowMs())
        }
    }

    internal fun entryCountForTest(): Int = entryCount.get()

    internal fun currentThreadDepthForTest(): Int = stack.get()?.depth ?: 0

    private fun recordEdge(
        callerId: Long,
        callerName: String?,
        calleeId: Long,
        calleeName: String?,
        durationMs: Long,
        expectedEpoch: Long,
    ) {
        val hash = edgeHash(callerId, calleeId)
        val shard = shards[hash and (SHARD_COUNT - 1)]
        synchronized(shard.lock) {
            if (epoch.get() != expectedEpoch) return
            val existing = shard.find(callerId, calleeId, hash)
            if (existing >= 0) {
                shard.update(existing, durationMs)
                return
            }

            val limit = maxKeys().coerceAtLeast(0)
            if (!reserveEntry(limit)) {
                capacityLoss.incrementAndGet()
                return
            }
            try {
                val context = captureContext()
                shard.insert(
                    callerId = callerId,
                    callerName = callerName,
                    calleeId = calleeId,
                    calleeName = calleeName,
                    hash = hash,
                    screen = context.screen,
                    flow = context.flow,
                    step = context.step,
                    durationMs = durationMs,
                )
            } catch (throwable: Throwable) {
                entryCount.decrementAndGet()
                capacityLoss.incrementAndGet()
                throw throwable
            }
        }
    }

    private fun reserveEntry(limit: Int): Boolean {
        if (limit <= 0) return false
        while (true) {
            val current = entryCount.get()
            if (current >= limit) return false
            if (entryCount.compareAndSet(current, current + 1)) return true
        }
    }

    private fun maybeFlush(now: Long, writer: AsyncLogWriter) {
        val last = lastFlushAtMs.get()
        if (now - last < RUNTIME_CALL_FLUSH_MS) return
        if (!lastFlushAtMs.compareAndSet(last, now)) return
        flushAt(now, force = true, writer)
    }

    private fun flushAt(now: Long, force: Boolean, writer: AsyncLogWriter) {
        if (!force) {
            val last = lastFlushAtMs.get()
            if (now - last < RUNTIME_CALL_FLUSH_MS) return
            if (!lastFlushAtMs.compareAndSet(last, now)) return
        }
        synchronized(flushLock) {
            val batch = takeBatch()
            if (batch != null) {
                writer.runtimeCalls(batch)
            }
            flushQuality(writer)
            val nextDelay = if (entryCount.get() > 0) RUNTIME_CALL_DRAIN_INTERVAL_MS else RUNTIME_CALL_FLUSH_MS
            lastFlushAtMs.set(now - (RUNTIME_CALL_FLUSH_MS - nextDelay))
        }
    }

    private fun takeBatch(): RuntimeCallBatch? {
        if (entryCount.get() <= 0) return null
        val batch = RuntimeCallBatch(MAX_FLUSH_RECORDS)
        var visited = 0
        while (visited < SHARD_COUNT && batch.size < MAX_FLUSH_RECORDS) {
            val shardIndex = nextFlushShard
            nextFlushShard = (nextFlushShard + 1) and (SHARD_COUNT - 1)
            val shard = shards[shardIndex]
            val removed = synchronized(shard.lock) {
                shard.drainInto(batch, MAX_FLUSH_RECORDS)
            }
            if (removed > 0) entryCount.addAndGet(-removed)
            visited++
        }
        return batch.takeIf { it.size > 0 }
    }

    private fun flushQuality(writer: AsyncLogWriter) {
        val capacityLossCount = capacityLoss.getAndSet(0L)
        if (capacityLossCount > 0L) {
            writer.recordQuality(QualityCounterId.RUNTIME_GRAPH_CAPACITY_LOSS, capacityLossCount)
        }
        val mismatchCount = stackMismatch.getAndSet(0L)
        if (mismatchCount > 0L) {
            writer.recordQuality(QualityCounterId.RUNTIME_STACK_MISMATCH, mismatchCount)
        }
    }

    private fun advanceEpoch() {
        while (true) {
            val current = epoch.get()
            val next = if (current == Long.MAX_VALUE) INITIAL_EPOCH else current + 1L
            if (epoch.compareAndSet(current, next)) return
        }
    }

    private class RuntimeCallStack(initialEpoch: Long) {
        var epoch: Long = initialEpoch
            private set
        private var values = LongArray(INITIAL_STACK_DEPTH * FRAME_WIDTH)
        private var names = arrayOfNulls<String>(INITIAL_STACK_DEPTH)

        var depth: Int = 0
            private set
        var poppedStartedAtMs: Long = 0L
            private set
        var poppedParentId: Long = 0L
            private set
        var poppedName: String? = null
            private set
        var poppedParentName: String? = null
            private set
        var hasPoppedParent: Boolean = false
            private set

        fun reset(newEpoch: Long) {
            names.fill(null, 0, depth)
            epoch = newEpoch
            depth = 0
            poppedStartedAtMs = 0L
            poppedParentId = 0L
            poppedName = null
            poppedParentName = null
            hasPoppedParent = false
        }

        fun push(methodId: Long, methodName: String?, startedAtMs: Long) {
            ensureCapacity(depth + 1)
            val offset = depth * FRAME_WIDTH
            values[offset] = methodId
            values[offset + 1] = startedAtMs
            names[depth] = methodName
            depth++
        }

        fun pop(methodId: Long): Boolean {
            hasPoppedParent = false
            poppedName = null
            poppedParentName = null
            if (depth <= 0) return false
            val topOffset = (depth - 1) * FRAME_WIDTH
            if (values[topOffset] == methodId) {
                poppedStartedAtMs = values[topOffset + 1]
                poppedName = names[depth - 1]
                names[depth - 1] = null
                depth--
                if (depth > 0) {
                    poppedParentId = values[(depth - 1) * FRAME_WIDTH]
                    poppedParentName = names[depth - 1]
                    hasPoppedParent = true
                }
                return true
            }

            // Truncate through the matching frame. A non-LIFO exit never emits a guessed edge.
            for (index in depth - 2 downTo 0) {
                if (values[index * FRAME_WIDTH] == methodId) {
                    names.fill(null, index, depth)
                    depth = index
                    return false
                }
            }
            names.fill(null, 0, depth)
            depth = 0
            return false
        }

        private fun ensureCapacity(requiredDepth: Int) {
            val required = requiredDepth * FRAME_WIDTH
            if (required <= values.size) return
            var newSize = values.size
            while (newSize < required) newSize = newSize shl 1
            values = values.copyOf(newSize)
            names = names.copyOf(newSize / FRAME_WIDTH)
        }
    }

    private class EdgeShard {
        val lock = Any()
        private var states = ByteArray(INITIAL_EDGE_CAPACITY)
        private var hashes = IntArray(INITIAL_EDGE_CAPACITY)
        private var callers = LongArray(INITIAL_EDGE_CAPACITY)
        private var callerNames = arrayOfNulls<String>(INITIAL_EDGE_CAPACITY)
        private var callees = LongArray(INITIAL_EDGE_CAPACITY)
        private var calleeNames = arrayOfNulls<String>(INITIAL_EDGE_CAPACITY)
        private var counts = LongArray(INITIAL_EDGE_CAPACITY)
        private var totalsMs = LongArray(INITIAL_EDGE_CAPACITY)
        private var maximaMs = LongArray(INITIAL_EDGE_CAPACITY)
        private var screens = arrayOfNulls<String>(INITIAL_EDGE_CAPACITY)
        private var flows = arrayOfNulls<String>(INITIAL_EDGE_CAPACITY)
        private var steps = arrayOfNulls<String>(INITIAL_EDGE_CAPACITY)
        private var size = 0
        private var used = 0
        private var drainCursor = 0

        fun find(callerId: Long, calleeId: Long, hash: Int): Int {
            var index = hash and (states.size - 1)
            repeat(states.size) {
                when (states[index]) {
                    EMPTY -> return -1
                    OCCUPIED -> if (
                        hashes[index] == hash && callers[index] == callerId && callees[index] == calleeId
                    ) {
                        return index
                    }
                }
                index = (index + 1) and (states.size - 1)
            }
            return -1
        }

        fun update(index: Int, durationMs: Long) {
            counts[index] = saturatingAdd(counts[index], 1L)
            totalsMs[index] = saturatingAdd(totalsMs[index], durationMs)
            if (durationMs > maximaMs[index]) maximaMs[index] = durationMs
        }

        fun insert(
            callerId: Long,
            callerName: String?,
            calleeId: Long,
            calleeName: String?,
            hash: Int,
            screen: String?,
            flow: String?,
            step: String?,
            durationMs: Long,
        ) {
            ensureInsertCapacity()
            var index = hash and (states.size - 1)
            var deleted = -1
            while (true) {
                when (states[index]) {
                    EMPTY -> {
                        if (deleted >= 0) index = deleted else used++
                        break
                    }
                    DELETED -> if (deleted < 0) deleted = index
                }
                index = (index + 1) and (states.size - 1)
            }
            states[index] = OCCUPIED
            hashes[index] = hash
            callers[index] = callerId
            callerNames[index] = callerName
            callees[index] = calleeId
            calleeNames[index] = calleeName
            counts[index] = 1L
            totalsMs[index] = durationMs
            maximaMs[index] = durationMs
            screens[index] = screen
            flows[index] = flow
            steps[index] = step
            size++
        }

        fun drainInto(batch: RuntimeCallBatch, limit: Int): Int {
            if (size <= 0 || batch.size >= limit) return 0
            var removed = 0
            var visited = 0
            var index = drainCursor and (states.size - 1)
            while (visited < states.size && batch.size < limit) {
                if (states[index] == OCCUPIED) {
                    batch.add(
                        screens[index],
                        callers[index],
                        callerNames[index],
                        flows[index],
                        steps[index],
                        callees[index],
                        calleeNames[index],
                        counts[index],
                        totalsMs[index],
                        maximaMs[index],
                    )
                    delete(index)
                    removed++
                }
                index = (index + 1) and (states.size - 1)
                visited++
            }
            drainCursor = index
            if (size == 0) resetEmptyTable()
            return removed
        }

        fun clear() {
            states.fill(EMPTY)
            hashes.fill(0)
            callers.fill(0L)
            callerNames.fill(null)
            callees.fill(0L)
            calleeNames.fill(null)
            counts.fill(0L)
            totalsMs.fill(0L)
            maximaMs.fill(0L)
            screens.fill(null)
            flows.fill(null)
            steps.fill(null)
            size = 0
            used = 0
            drainCursor = 0
        }

        private fun delete(index: Int) {
            states[index] = DELETED
            counts[index] = 0L
            totalsMs[index] = 0L
            maximaMs[index] = 0L
            callerNames[index] = null
            calleeNames[index] = null
            screens[index] = null
            flows[index] = null
            steps[index] = null
            size--
        }

        private fun resetEmptyTable() {
            states.fill(EMPTY)
            used = 0
            drainCursor = 0
        }

        private fun ensureInsertCapacity() {
            if ((used + 1) * LOAD_FACTOR_DENOMINATOR < states.size * LOAD_FACTOR_NUMERATOR) return
            val compact = size * 2 < used
            rehash(if (compact) states.size else states.size shl 1)
        }

        private fun rehash(capacity: Int) {
            val oldStates = states
            val oldHashes = hashes
            val oldCallers = callers
            val oldCallerNames = callerNames
            val oldCallees = callees
            val oldCalleeNames = calleeNames
            val oldCounts = counts
            val oldTotals = totalsMs
            val oldMaxima = maximaMs
            val oldScreens = screens
            val oldFlows = flows
            val oldSteps = steps

            states = ByteArray(capacity)
            hashes = IntArray(capacity)
            callers = LongArray(capacity)
            callerNames = arrayOfNulls(capacity)
            callees = LongArray(capacity)
            calleeNames = arrayOfNulls(capacity)
            counts = LongArray(capacity)
            totalsMs = LongArray(capacity)
            maximaMs = LongArray(capacity)
            screens = arrayOfNulls(capacity)
            flows = arrayOfNulls(capacity)
            steps = arrayOfNulls(capacity)
            used = 0
            size = 0
            drainCursor = 0

            for (oldIndex in oldStates.indices) {
                if (oldStates[oldIndex] != OCCUPIED) continue
                var index = oldHashes[oldIndex] and (capacity - 1)
                while (states[index] == OCCUPIED) index = (index + 1) and (capacity - 1)
                states[index] = OCCUPIED
                hashes[index] = oldHashes[oldIndex]
                callers[index] = oldCallers[oldIndex]
                callerNames[index] = oldCallerNames[oldIndex]
                callees[index] = oldCallees[oldIndex]
                calleeNames[index] = oldCalleeNames[oldIndex]
                counts[index] = oldCounts[oldIndex]
                totalsMs[index] = oldTotals[oldIndex]
                maximaMs[index] = oldMaxima[oldIndex]
                screens[index] = oldScreens[oldIndex]
                flows[index] = oldFlows[oldIndex]
                steps[index] = oldSteps[oldIndex]
                used++
                size++
            }
        }
    }

    private companion object {
        const val DISABLED_TOKEN = 0L
        const val INITIAL_EPOCH = 1L
        const val FRAME_WIDTH = 2
        const val INITIAL_STACK_DEPTH = 16
        const val SHARD_COUNT = 16
        const val INITIAL_EDGE_CAPACITY = 16
        const val MAX_FLUSH_RECORDS = 128
        const val RUNTIME_CALL_FLUSH_MS = 5_000L
        const val RUNTIME_CALL_DRAIN_INTERVAL_MS = 250L
        const val LOAD_FACTOR_NUMERATOR = 3
        const val LOAD_FACTOR_DENOMINATOR = 4
        const val EMPTY: Byte = 0
        const val OCCUPIED: Byte = 1
        const val DELETED: Byte = 2

        fun edgeHash(callerId: Long, calleeId: Long): Int {
            var mixed = callerId xor java.lang.Long.rotateLeft(calleeId, 29)
            mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
            mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
            return (mixed xor (mixed ushr 32)).toInt()
        }

        fun saturatingAdd(left: Long, right: Long): Long {
            if (right <= 0L) return left
            return if (left > Long.MAX_VALUE - right) Long.MAX_VALUE else left + right
        }
    }
}
