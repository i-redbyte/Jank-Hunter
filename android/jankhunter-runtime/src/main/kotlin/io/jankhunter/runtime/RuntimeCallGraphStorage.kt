package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.concurrent.SpscSlotSequencer
import io.jankhunter.runtime.internal.io.RuntimeCallBatch
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicLong

// Eight bounded pages absorb short producer bursts while the dedicated consumer is descheduled.
// The 128-slot power-of-two table keeps bit-mask probing correct and its 75% load limit carries
// 96 unique edges per publication. This layout doubles burst capacity while using only 4/3 of the
// memory of the former four-page, incorrectly sized 192-slot layout.
internal const val RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY = 8
internal const val RUNTIME_GRAPH_PAGE_MAX_KEYS = 96
internal const val RUNTIME_GRAPH_PAGE_TABLE_CAPACITY = 128
internal const val RUNTIME_GRAPH_MAX_FLUSH_RECORDS = 128
internal const val RUNTIME_GRAPH_ADD_FULL = 0
internal const val RUNTIME_GRAPH_ADD_AGGREGATED = 1
internal const val RUNTIME_GRAPH_ADD_PAGE_PUBLISHED = 2

internal class RuntimeCallStack {
    private var ids = LongArray(INITIAL_DEPTH)
    private var startedAtMs = LongArray(INITIAL_DEPTH)
    private var names = arrayOfNulls<String>(INITIAL_DEPTH)
    private var screens = arrayOfNulls<String>(INITIAL_DEPTH)
    private var operationIds = LongArray(INITIAL_DEPTH)

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
    var poppedOperationId: Long = 0L
        private set
    var hasPoppedParent = false
        private set

    fun push(
        methodId: Long,
        methodName: String,
        startedAtMs: Long,
        screen: String?,
        operationId: Long,
    ) {
        ensureCapacity(depth + 1)
        ids[depth] = methodId
        this.startedAtMs[depth] = startedAtMs
        names[depth] = methodName
        screens[depth] = screen
        operationIds[depth] = operationId
        depth++
    }

    fun hasCurrentMethod(): Boolean = depth > 0

    fun currentMethodId(): Long = if (depth > 0) ids[depth - 1] else 0L

    fun currentMethodName(): String? = if (depth > 0) names[depth - 1] else null

    private fun ensureCapacity(required: Int) {
        if (required <= ids.size) return
        val capacity = ids.size shl 1
        ids = ids.copyOf(capacity)
        startedAtMs = startedAtMs.copyOf(capacity)
        names = names.copyOf(capacity)
        screens = screens.copyOf(capacity)
        operationIds = operationIds.copyOf(capacity)
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
        poppedOperationId = operationIds[index]
    }

    private fun clearRange(from: Int, until: Int) {
        for (index in from until until) clearFrame(index)
    }

    private fun clearFrame(index: Int) {
        names[index] = null
        screens[index] = null
        operationIds[index] = 0L
    }

    private fun clearPopped() {
        poppedStartedAtMs = 0L
        poppedParentId = 0L
        poppedName = null
        poppedParentName = null
        poppedScreen = null
        poppedOperationId = 0L
        hasPoppedParent = false
    }

    private companion object {
        const val INITIAL_DEPTH = 64
    }
}

internal class RuntimeGraphAggregateBuffer(thread: Thread) {
    val owner = WeakReference(thread)
    private val pages = Array(RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY) { RuntimeGraphAggregatePage() }
    private val sequencer = SpscSlotSequencer(RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY)
    private val publishedLogicalEvents = AtomicLong()
    private var producerPosition = SpscSlotSequencer.NO_POSITION

    @Volatile private var producerAttempted = 0L
    @Volatile private var producerAccepted = 0L

    @Volatile var producerWaiting = false
    @Volatile var producerActive = false
    // Stays true while a producer temporarily yields producerActive to a consumer page rotation.
    @Volatile var producerAdmitted = false

    @Volatile var rotationRequested = false

    fun recordAttempted() {
        producerAttempted = saturatingAdd(producerAttempted, 1L)
    }

    fun recordAccepted() {
        producerAccepted = saturatingAdd(producerAccepted, 1L)
    }

    fun attemptedCount(): Long = producerAttempted

    fun acceptedCount(): Long = producerAccepted

    fun tryAdd(
        callerId: Long,
        callerName: String,
        calleeId: Long,
        calleeName: String,
        screen: String?,
        operationId: Long,
        durationMs: Long,
    ): Int {
        if (!ensureActivePage()) return RUNTIME_GRAPH_ADD_FULL
        var page = activePage()
        if (page.add(callerId, callerName, calleeId, calleeName, screen, operationId, durationMs)) {
            return RUNTIME_GRAPH_ADD_AGGREGATED
        }
        // Keep the last full page producer-owned while the ring is saturated. Hot edges already
        // present in that page can still be aggregated without races or extra memory; a later new
        // edge will rotate the page as soon as the consumer releases a slot.
        if (!sequencer.canAdvanceProducerAfterPublish(producerPosition)) return RUNTIME_GRAPH_ADD_FULL
        publishActivePage()
        if (!ensureActivePage()) return RUNTIME_GRAPH_ADD_FULL
        page = activePage()
        check(page.add(callerId, callerName, calleeId, calleeName, screen, operationId, durationMs)) {
            "Runtime graph edge did not fit an empty producer page"
        }
        return RUNTIME_GRAPH_ADD_PAGE_PUBLISHED
    }

    fun publishActivePage(): Boolean {
        if (producerPosition == SpscSlotSequencer.NO_POSITION) return false
        val page = activePage()
        if (page.size == 0) return false
        publishedLogicalEvents.addAndGet(page.logicalEventCount())
        sequencer.publish(producerPosition)
        producerPosition = SpscSlotSequencer.NO_POSITION
        return true
    }

    fun tryClaimConsumer(): Long = sequencer.tryClaimConsumer()

    fun pageAt(position: Long): RuntimeGraphAggregatePage = pages[sequencer.slotIndex(position)]

    fun release(position: Long) {
        val page = pageAt(position)
        publishedLogicalEvents.addAndGet(-page.logicalEventCount())
        page.clear()
        sequencer.release(position)
    }

    fun hasPublishedPages(): Boolean = !sequencer.isEmpty()

    fun hasActiveData(): Boolean {
        return producerPosition != SpscSlotSequencer.NO_POSITION && activePage().size > 0
    }

    fun bufferedLogicalEventCount(): Long {
        val published = publishedLogicalEvents.get().coerceAtLeast(0L)
        val active = if (producerPosition == SpscSlotSequencer.NO_POSITION) 0L else activePage().logicalEventCount()
        return saturatingAdd(published, active)
    }

    fun clear() {
        pages.forEach(RuntimeGraphAggregatePage::clear)
        publishedLogicalEvents.set(0L)
        producerPosition = SpscSlotSequencer.NO_POSITION
        producerWaiting = false
        producerActive = false
        producerAdmitted = false
        rotationRequested = false
        producerAttempted = 0L
        producerAccepted = 0L
    }

    private fun ensureActivePage(): Boolean {
        if (producerPosition != SpscSlotSequencer.NO_POSITION) return true
        val position = sequencer.tryClaimProducer()
        if (position == SpscSlotSequencer.NO_POSITION) return false
        producerPosition = position
        return true
    }

    private fun activePage(): RuntimeGraphAggregatePage {
        check(producerPosition != SpscSlotSequencer.NO_POSITION) { "Runtime graph producer page is not claimed" }
        return pageAt(producerPosition)
    }
}

internal class RuntimeGraphAggregatePage {
    private val states = ByteArray(TABLE_CAPACITY)
    private val hashes = IntArray(TABLE_CAPACITY)
    val callers = LongArray(TABLE_CAPACITY)
    val callerNames = arrayOfNulls<String>(TABLE_CAPACITY)
    val callees = LongArray(TABLE_CAPACITY)
    val calleeNames = arrayOfNulls<String>(TABLE_CAPACITY)
    val screens = arrayOfNulls<String>(TABLE_CAPACITY)
    val operationIds = LongArray(TABLE_CAPACITY)
    val counts = LongArray(TABLE_CAPACITY)
    val totalsMs = LongArray(TABLE_CAPACITY)
    val maximaMs = LongArray(TABLE_CAPACITY)

    var size = 0
        private set
    private var lastIndex = -1
    private var logicalEvents = 0L

    fun add(
        callerId: Long,
        callerName: String,
        calleeId: Long,
        calleeName: String,
        screen: String?,
        operationId: Long,
        durationMs: Long,
    ): Boolean {
        val cached = lastIndex
        if (cached >= 0 && matches(cached, callerId, calleeId, screen, operationId)) {
            updateNames(cached, callerName, calleeName)
            merge(cached, 1L, durationMs, durationMs)
            logicalEvents = saturatingAdd(logicalEvents, 1L)
            return true
        }
        val hash = runtimeGraphEdgeHash(callerId, calleeId, screen, operationId)
        var index = hash and TABLE_MASK
        repeat(TABLE_CAPACITY) {
            if (states[index] == EMPTY) {
                if (size >= RUNTIME_GRAPH_PAGE_MAX_KEYS) return false
                states[index] = OCCUPIED
                hashes[index] = hash
                callers[index] = callerId
                callerNames[index] = callerName
                callees[index] = calleeId
                calleeNames[index] = calleeName
                screens[index] = screen
                operationIds[index] = operationId
                counts[index] = 1L
                totalsMs[index] = durationMs
                maximaMs[index] = durationMs
                size++
                lastIndex = index
                logicalEvents = saturatingAdd(logicalEvents, 1L)
                return true
            }
            if (hashes[index] == hash && matches(index, callerId, calleeId, screen, operationId)) {
                updateNames(index, callerName, calleeName)
                merge(index, 1L, durationMs, durationMs)
                lastIndex = index
                logicalEvents = saturatingAdd(logicalEvents, 1L)
                return true
            }
            index = (index + 1) and TABLE_MASK
        }
        return false
    }

    fun nextOccupiedIndex(from: Int): Int {
        for (index in from.coerceAtLeast(0) until TABLE_CAPACITY) {
            if (states[index] == OCCUPIED) return index
        }
        return -1
    }

    fun logicalEventCount(): Long = logicalEvents

    fun clear() {
        var index = nextOccupiedIndex(0)
        while (index >= 0) {
            states[index] = EMPTY
            callerNames[index] = null
            calleeNames[index] = null
            screens[index] = null
            operationIds[index] = 0L
            counts[index] = 0L
            totalsMs[index] = 0L
            maximaMs[index] = 0L
            index = nextOccupiedIndex(index + 1)
        }
        size = 0
        lastIndex = -1
        logicalEvents = 0L
    }

    private fun matches(
        index: Int,
        callerId: Long,
        calleeId: Long,
        screen: String?,
        operationId: Long,
    ): Boolean {
        return callers[index] == callerId &&
            callees[index] == calleeId &&
            screens[index] == screen &&
            operationIds[index] == operationId
    }

    private fun merge(index: Int, count: Long, totalMs: Long, maxMs: Long) {
        counts[index] = saturatingAdd(counts[index], count)
        totalsMs[index] = saturatingAdd(totalsMs[index], totalMs)
        if (maxMs > maximaMs[index]) maximaMs[index] = maxMs
    }

    private fun updateNames(index: Int, callerName: String, calleeName: String) {
        if (callerNames[index] == null) callerNames[index] = callerName
        if (calleeNames[index] == null) calleeNames[index] = calleeName
    }

    private companion object {
        const val TABLE_CAPACITY = RUNTIME_GRAPH_PAGE_TABLE_CAPACITY
        const val TABLE_MASK = TABLE_CAPACITY - 1
        const val EMPTY: Byte = 0
        const val OCCUPIED: Byte = 1
    }
}

internal class RuntimeGraphEdgeTable {
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
    private var operationIds = LongArray(INITIAL_CAPACITY)
    var size = 0
        private set
    private var used = 0
    private var drainCursor = 0

    fun add(page: RuntimeGraphAggregatePage, source: Int, limit: Int): Boolean {
        val hash = runtimeGraphEdgeHash(
            page.callers[source],
            page.callees[source],
            page.screens[source],
            page.operationIds[source],
        )
        val existing = find(page, source, hash)
        if (existing >= 0) {
            if (callerNames[existing] == null && page.callerNames[source] != null) {
                callerNames[existing] = page.callerNames[source]
            }
            if (calleeNames[existing] == null && page.calleeNames[source] != null) {
                calleeNames[existing] = page.calleeNames[source]
            }
            counts[existing] = saturatingAdd(counts[existing], page.counts[source])
            totalsMs[existing] = saturatingAdd(totalsMs[existing], page.totalsMs[source])
            if (page.maximaMs[source] > maximaMs[existing]) maximaMs[existing] = page.maximaMs[source]
            return true
        }
        if (limit <= 0 || size >= limit) return false
        ensureInsertCapacity()
        val index = findInsertIndex(hash)
        if (states[index] == EMPTY) used++
        states[index] = OCCUPIED
        hashes[index] = hash
        callers[index] = page.callers[source]
        callerNames[index] = page.callerNames[source]
        callees[index] = page.callees[source]
        calleeNames[index] = page.calleeNames[source]
        screens[index] = page.screens[source]
        operationIds[index] = page.operationIds[source]
        counts[index] = page.counts[source]
        totalsMs[index] = page.totalsMs[source]
        maximaMs[index] = page.maximaMs[source]
        size++
        return true
    }

    fun drainInto(batch: RuntimeCallBatch) {
        var visited = 0
        var index = drainCursor and (states.size - 1)
        while (visited < states.size && batch.size < RUNTIME_GRAPH_MAX_FLUSH_RECORDS) {
            if (states[index] == OCCUPIED) {
                batch.add(
                    screens[index], callers[index], checkNotNull(callerNames[index]), operationIds[index],
                    callees[index], checkNotNull(calleeNames[index]), counts[index], totalsMs[index], maximaMs[index],
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

    private fun find(page: RuntimeGraphAggregatePage, source: Int, hash: Int): Int {
        var index = hash and (states.size - 1)
        repeat(states.size) {
            when (states[index]) {
                EMPTY -> return -1
                OCCUPIED -> if (matches(page, source, index, hash)) return index
            }
            index = (index + 1) and (states.size - 1)
        }
        return -1
    }

    private fun matches(page: RuntimeGraphAggregatePage, source: Int, index: Int, hash: Int): Boolean {
        return hashes[index] == hash &&
            callers[index] == page.callers[source] &&
            callees[index] == page.callees[source] &&
            screens[index] == page.screens[source] &&
            operationIds[index] == page.operationIds[source]
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
        operationIds[index] = 0L
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
            screens, operationIds,
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
        operationIds = LongArray(capacity)
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
        operationIds[to] = old.operationIds[from]
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
        val operationIds: LongArray,
    )

    private companion object {
        const val INITIAL_CAPACITY = 16
        const val LOAD_NUMERATOR = 3
        const val LOAD_DENOMINATOR = 4
        const val EMPTY: Byte = 0
        const val OCCUPIED: Byte = 1
        const val DELETED: Byte = 2

    }
}

private fun runtimeGraphEdgeHash(caller: Long, callee: Long, screen: String?, operationId: Long): Int {
    var mixed = caller xor java.lang.Long.rotateLeft(callee, 29)
    mixed = mixed xor ((screen?.hashCode() ?: 0).toLong() shl 32)
    mixed = mixed xor java.lang.Long.rotateLeft(operationId, 17)
    mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
    mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
    return (mixed xor (mixed ushr 32)).toInt()
}
