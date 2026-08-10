package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.lang.ref.WeakReference

internal const val RUNTIME_GRAPH_BUFFER_CAPACITY = 256
internal const val RUNTIME_GRAPH_MAX_FLUSH_RECORDS = 128

internal class RuntimeCallStack {
    private val ids = LongArray(MAX_DEPTH)
    private val startedAtMs = LongArray(MAX_DEPTH)
    private val names = arrayOfNulls<String>(MAX_DEPTH)
    private val screens = arrayOfNulls<String>(MAX_DEPTH)
    private val flows = arrayOfNulls<String>(MAX_DEPTH)
    private val steps = arrayOfNulls<String>(MAX_DEPTH)

    var depth: Int = 0
        private set
    var poppedStartedAtMs = 0L
        private set
    var poppedParentId = 0L
        private set
    var poppedName: String? = null
        private set
    var poppedParentName: String? = null
        private set
    var poppedScreen: String? = null
        private set
    var poppedFlow: String? = null
        private set
    var poppedStep: String? = null
        private set
    var hasPoppedParent = false
        private set

    fun push(
        methodId: Long,
        methodName: String?,
        startedAtMs: Long,
        screen: String?,
        flow: String?,
        step: String?,
    ): Boolean {
        if (depth >= ids.size) return false
        ids[depth] = methodId
        this.startedAtMs[depth] = startedAtMs
        names[depth] = methodName
        screens[depth] = screen
        flows[depth] = flow
        steps[depth] = step
        depth++
        return true
    }

    /** Returns false and discards unmatched inner frames when exits arrive out of LIFO order. */
    fun pop(methodId: Long): Boolean {
        clearPopped()
        if (depth <= 0) return false
        val top = depth - 1
        if (ids[top] == methodId) {
            capturePopped(top)
            clearFrame(top)
            depth = top
            if (depth > 0) {
                poppedParentId = ids[depth - 1]
                poppedParentName = names[depth - 1]
                hasPoppedParent = true
            }
            return true
        }
        for (index in depth - 2 downTo 0) {
            if (ids[index] == methodId) {
                clearRange(index, depth)
                depth = index
                return false
            }
        }
        reset()
        return false
    }

    fun reset() {
        clearRange(0, depth)
        depth = 0
        clearPopped()
    }

    private fun capturePopped(index: Int) {
        poppedStartedAtMs = startedAtMs[index]
        poppedName = names[index]
        poppedScreen = screens[index]
        poppedFlow = flows[index]
        poppedStep = steps[index]
    }

    private fun clearRange(from: Int, until: Int) {
        for (index in from until until) clearFrame(index)
    }

    private fun clearFrame(index: Int) {
        names[index] = null
        screens[index] = null
        flows[index] = null
        steps[index] = null
    }

    private fun clearPopped() {
        poppedStartedAtMs = 0L
        poppedParentId = 0L
        poppedName = null
        poppedParentName = null
        poppedScreen = null
        poppedFlow = null
        poppedStep = null
        hasPoppedParent = false
    }

    private companion object {
        const val MAX_DEPTH = 256
    }
}

internal class RuntimeGraphEdgeBuffer(thread: Thread) {
    val owner = WeakReference(thread)
    val sequencer = SpscSlotSequencer(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val epochs = LongArray(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val callers = LongArray(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val callerNames = arrayOfNulls<String>(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val callees = LongArray(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val calleeNames = arrayOfNulls<String>(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val screens = arrayOfNulls<String>(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val flows = arrayOfNulls<String>(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val steps = arrayOfNulls<String>(RUNTIME_GRAPH_BUFFER_CAPACITY)
    val durationsMs = LongArray(RUNTIME_GRAPH_BUFFER_CAPACITY)

    fun publish(
        eventEpoch: Long,
        callerId: Long,
        callerName: String?,
        calleeId: Long,
        calleeName: String?,
        screen: String?,
        flow: String?,
        step: String?,
        durationMs: Long,
    ): Boolean {
        val position = sequencer.tryClaimProducer()
        if (position == SpscSlotSequencer.NO_POSITION) return false
        val slot = sequencer.slotIndex(position)
        epochs[slot] = eventEpoch
        callers[slot] = callerId
        callerNames[slot] = callerName
        callees[slot] = calleeId
        calleeNames[slot] = calleeName
        screens[slot] = screen
        flows[slot] = flow
        steps[slot] = step
        durationsMs[slot] = durationMs
        sequencer.publish(position)
        return true
    }

    fun clearReferences(slot: Int) {
        callerNames[slot] = null
        calleeNames[slot] = null
        screens[slot] = null
        flows[slot] = null
        steps[slot] = null
    }

    fun clear() {
        callerNames.fill(null)
        calleeNames.fill(null)
        screens.fill(null)
        flows.fill(null)
        steps.fill(null)
    }
}

internal data class RuntimeGraphShadowComparisonResult(
    val missingEdges: Long,
    val extraEdges: Long,
    val countDifferences: Long,
    val durationDifferences: Long,
    val contextSplits: Long,
) {
    companion object {
        val EMPTY = RuntimeGraphShadowComparisonResult(0L, 0L, 0L, 0L, 0L)
    }
}

internal class RuntimeGraphShadowComparison {
    private val legacy = HashMap<PairKey, Aggregate>()
    private val buffered = HashMap<ContextKey, Aggregate>()

    fun record(buffer: RuntimeGraphEdgeBuffer, slot: Int, limit: Int): Boolean {
        val pair = PairKey(buffer.callers[slot], buffer.callees[slot])
        val context = ContextKey(pair, buffer.screens[slot], buffer.flows[slot], buffer.steps[slot])
        return add(legacy, pair, buffer.durationsMs[slot], limit) &&
            add(buffered, context, buffer.durationsMs[slot], limit)
    }

    fun compareAndClear(): RuntimeGraphShadowComparisonResult {
        if (legacy.isEmpty() && buffered.isEmpty()) return RuntimeGraphShadowComparisonResult.EMPTY
        val projected = HashMap<PairKey, Aggregate>()
        val contexts = HashMap<PairKey, Int>()
        buffered.forEach { (key, value) ->
            projected.getOrPut(key.pair, ::Aggregate).merge(value)
            contexts[key.pair] = (contexts[key.pair] ?: 0) + 1
        }
        var missing = 0L
        var countDifferences = 0L
        var durationDifferences = 0L
        legacy.forEach { (key, value) ->
            val other = projected[key]
            if (other == null) {
                missing++
            } else {
                if (value.count != other.count) countDifferences++
                if (value.totalMs != other.totalMs || value.maxMs != other.maxMs) durationDifferences++
            }
        }
        val result = RuntimeGraphShadowComparisonResult(
            missingEdges = missing,
            extraEdges = projected.keys.count { !legacy.containsKey(it) }.toLong(),
            countDifferences = countDifferences,
            durationDifferences = durationDifferences,
            contextSplits = contexts.values.count { it > 1 }.toLong(),
        )
        legacy.clear()
        buffered.clear()
        return result
    }

    private fun <K> add(target: MutableMap<K, Aggregate>, key: K, durationMs: Long, limit: Int): Boolean {
        val existing = target[key]
        if (existing != null) {
            existing.add(durationMs)
            return true
        }
        if (limit <= 0 || target.size >= limit) return false
        target[key] = Aggregate().also { it.add(durationMs) }
        return true
    }

    private data class PairKey(val callerId: Long, val calleeId: Long)

    private data class ContextKey(
        val pair: PairKey,
        val screen: String?,
        val flow: String?,
        val step: String?,
    )

    private class Aggregate {
        var count = 0L
        var totalMs = 0L
        var maxMs = 0L

        fun add(durationMs: Long) {
            count = saturatingAdd(count, 1L)
            totalMs = saturatingAdd(totalMs, durationMs)
            if (durationMs > maxMs) maxMs = durationMs
        }

        fun merge(other: Aggregate) {
            count = saturatingAdd(count, other.count)
            totalMs = saturatingAdd(totalMs, other.totalMs)
            if (other.maxMs > maxMs) maxMs = other.maxMs
        }
    }
}

internal class RuntimeGraphEdgeTable(
    private val contextAware: Boolean,
) {
    private var states = ByteArray(INITIAL_CAPACITY)
    private var hashes = IntArray(INITIAL_CAPACITY)
    private var callers = LongArray(INITIAL_CAPACITY)
    private var callerNames = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var callees = LongArray(INITIAL_CAPACITY)
    private var calleeNames = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var counts = LongArray(INITIAL_CAPACITY)
    private var totalsMs = LongArray(INITIAL_CAPACITY)
    private var maximaMs = LongArray(INITIAL_CAPACITY)
    private var screens = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var flows = arrayOfNulls<String>(INITIAL_CAPACITY)
    private var steps = arrayOfNulls<String>(INITIAL_CAPACITY)
    var size = 0
        private set
    private var used = 0
    private var drainCursor = 0

    fun add(buffer: RuntimeGraphEdgeBuffer, slot: Int, limit: Int): Boolean {
        val hash = edgeHash(
            buffer.callers[slot],
            buffer.callees[slot],
            buffer.screens[slot].takeIf { contextAware },
            buffer.flows[slot].takeIf { contextAware },
            buffer.steps[slot].takeIf { contextAware },
        )
        val existing = find(buffer, slot, hash)
        val duration = buffer.durationsMs[slot]
        if (existing >= 0) {
            counts[existing] = saturatingAdd(counts[existing], 1L)
            totalsMs[existing] = saturatingAdd(totalsMs[existing], duration)
            if (duration > maximaMs[existing]) maximaMs[existing] = duration
            return true
        }
        if (limit <= 0 || size >= limit) return false
        ensureInsertCapacity()
        val index = findInsertIndex(hash)
        if (states[index] == EMPTY) used++
        states[index] = OCCUPIED
        hashes[index] = hash
        callers[index] = buffer.callers[slot]
        callerNames[index] = buffer.callerNames[slot]
        callees[index] = buffer.callees[slot]
        calleeNames[index] = buffer.calleeNames[slot]
        screens[index] = buffer.screens[slot]
        flows[index] = buffer.flows[slot]
        steps[index] = buffer.steps[slot]
        counts[index] = 1L
        totalsMs[index] = duration
        maximaMs[index] = duration
        size++
        return true
    }

    fun drainInto(batch: RuntimeCallBatch) {
        var visited = 0
        var index = drainCursor and (states.size - 1)
        while (visited < states.size && batch.size < RUNTIME_GRAPH_MAX_FLUSH_RECORDS) {
            if (states[index] == OCCUPIED) {
                batch.add(
                    screens[index], callers[index], callerNames[index], flows[index], steps[index],
                    callees[index], calleeNames[index], counts[index], totalsMs[index], maximaMs[index],
                )
                delete(index)
            }
            index = (index + 1) and (states.size - 1)
            visited++
        }
        drainCursor = index
        if (size == 0) resetEmptyTable()
    }

    fun logicalEventCount(): Long {
        var result = 0L
        for (index in states.indices) {
            if (states[index] == OCCUPIED) result = saturatingAdd(result, counts[index])
        }
        return result
    }

    fun capacityForTest(): Int = states.size

    private fun find(buffer: RuntimeGraphEdgeBuffer, slot: Int, hash: Int): Int {
        var index = hash and (states.size - 1)
        repeat(states.size) {
            when (states[index]) {
                EMPTY -> return -1
                OCCUPIED -> if (matches(buffer, slot, index, hash)) return index
            }
            index = (index + 1) and (states.size - 1)
        }
        return -1
    }

    private fun matches(buffer: RuntimeGraphEdgeBuffer, slot: Int, index: Int, hash: Int): Boolean {
        return hashes[index] == hash &&
            callers[index] == buffer.callers[slot] &&
            callees[index] == buffer.callees[slot] &&
            (!contextAware || (
                screens[index] == buffer.screens[slot] &&
                    flows[index] == buffer.flows[slot] &&
                    steps[index] == buffer.steps[slot]
                ))
    }

    private fun findInsertIndex(hash: Int): Int {
        var index = hash and (states.size - 1)
        var deleted = -1
        while (true) {
            when (states[index]) {
                EMPTY -> return if (deleted >= 0) deleted else index
                DELETED -> if (deleted < 0) deleted = index
            }
            index = (index + 1) and (states.size - 1)
        }
    }

    private fun delete(index: Int) {
        states[index] = DELETED
        callerNames[index] = null
        calleeNames[index] = null
        screens[index] = null
        flows[index] = null
        steps[index] = null
        counts[index] = 0L
        totalsMs[index] = 0L
        maximaMs[index] = 0L
        size--
    }

    private fun resetEmptyTable() {
        states.fill(EMPTY)
        used = 0
        drainCursor = 0
    }

    private fun ensureInsertCapacity() {
        if ((used + 1) * LOAD_DENOMINATOR < states.size * LOAD_NUMERATOR) return
        val capacity = if ((size + 1) * LOAD_DENOMINATOR < states.size * LOAD_NUMERATOR) {
            states.size
        } else {
            states.size shl 1
        }
        rehash(capacity)
    }

    private fun rehash(capacity: Int) {
        val old = StorageSnapshot(
            states, hashes, callers, callerNames, callees, calleeNames, counts, totalsMs, maximaMs,
            screens, flows, steps,
        )
        allocate(capacity)
        for (oldIndex in old.states.indices) {
            if (old.states[oldIndex] != OCCUPIED) continue
            val index = findInsertIndex(old.hashes[oldIndex])
            copyEntry(old, oldIndex, index)
            size++
            used++
        }
    }

    private fun allocate(capacity: Int) {
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
        size = 0
        used = 0
        drainCursor = 0
    }

    private fun copyEntry(old: StorageSnapshot, from: Int, to: Int) {
        states[to] = OCCUPIED
        hashes[to] = old.hashes[from]
        callers[to] = old.callers[from]
        callerNames[to] = old.callerNames[from]
        callees[to] = old.callees[from]
        calleeNames[to] = old.calleeNames[from]
        counts[to] = old.counts[from]
        totalsMs[to] = old.totalsMs[from]
        maximaMs[to] = old.maximaMs[from]
        screens[to] = old.screens[from]
        flows[to] = old.flows[from]
        steps[to] = old.steps[from]
    }

    private class StorageSnapshot(
        val states: ByteArray,
        val hashes: IntArray,
        val callers: LongArray,
        val callerNames: Array<String?>,
        val callees: LongArray,
        val calleeNames: Array<String?>,
        val counts: LongArray,
        val totalsMs: LongArray,
        val maximaMs: LongArray,
        val screens: Array<String?>,
        val flows: Array<String?>,
        val steps: Array<String?>,
    )

    private companion object {
        const val INITIAL_CAPACITY = 16
        const val LOAD_NUMERATOR = 3
        const val LOAD_DENOMINATOR = 4
        const val EMPTY: Byte = 0
        const val OCCUPIED: Byte = 1
        const val DELETED: Byte = 2

        fun edgeHash(caller: Long, callee: Long, screen: String?, flow: String?, step: String?): Int {
            var mixed = caller xor java.lang.Long.rotateLeft(callee, 29)
            mixed = mixed xor ((screen?.hashCode() ?: 0).toLong() shl 32)
            mixed = mixed xor (flow?.hashCode() ?: 0).toLong()
            mixed = mixed xor java.lang.Long.rotateLeft((step?.hashCode() ?: 0).toLong(), 17)
            mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
            mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
            return (mixed xor (mixed ushr 32)).toInt()
        }
    }
}
