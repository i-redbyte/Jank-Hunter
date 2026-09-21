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

/** Initial stack storage is reserved together with producer metadata before construction. */
internal class RuntimeCallStack(
    private val storageBudget: RuntimeGraphStorageBudget? = null,
    private val nextSkippedToken: RuntimeLongSource = RuntimeLongSource { 0L },
) {
    private var ids = LongArray(INITIAL_DEPTH)
    private var startedAtMs = LongArray(INITIAL_DEPTH)
    private var names = arrayOfNulls<String>(INITIAL_DEPTH)
    private var screens = arrayOfNulls<String>(INITIAL_DEPTH)
    private var operationIds = LongArray(INITIAL_DEPTH)
    private var chargedBytes = if (storageBudget == null) 0L else RuntimeGraphStorageBudget.stackBytes(INITIAL_DEPTH)
    private var skippedDepth = 0L
    private var skippedOverflow = false
    private var released = false
    var skippedToken = 0L
        private set

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
    ): Boolean {
        if (released) return false
        if (skippedDepth > 0L || !ensureCapacity(depth + 1)) {
            if (skippedDepth == 0L) skippedToken = nextSkippedToken.getAsLong()
            if (skippedDepth < Long.MAX_VALUE) skippedDepth++ else skippedOverflow = true
            if (skippedToken == 0L) skippedOverflow = true
            return false
        }
        ids[depth] = methodId
        this.startedAtMs[depth] = startedAtMs
        names[depth] = methodName
        screens[depth] = screen
        operationIds[depth] = operationId
        depth++
        return true
    }

    fun hasCurrentMethod(): Boolean = depth > 0 && skippedDepth == 0L

    fun currentMethodId(): Long = if (hasCurrentMethod()) ids[depth - 1] else 0L

    fun currentMethodName(): String? = if (hasCurrentMethod()) names[depth - 1] else null

    private fun ensureCapacity(required: Int): Boolean {
        if (required <= ids.size) return true
        if (ids.size > Int.MAX_VALUE / 2) return false
        val newCapacity = maxOf(INITIAL_DEPTH, ids.size shl 1)
        val bytes = RuntimeGraphStorageBudget.stackBytes(newCapacity)
        if (storageBudget != null && !storageBudget.tryReserve(bytes)) return false
        try {
            // Keep the old complete set if any allocation fails; the temporary set is also charged.
            val newIds = ids.copyOf(newCapacity)
            val newStarted = startedAtMs.copyOf(newCapacity)
            val newNames = names.copyOf(newCapacity)
            val newScreens = screens.copyOf(newCapacity)
            val newOperations = operationIds.copyOf(newCapacity)
            ids = newIds
            startedAtMs = newStarted
            names = newNames
            screens = newScreens
            operationIds = newOperations
        } catch (failure: Throwable) {
            storageBudget?.release(bytes)
            throw failure
        }
        if (storageBudget != null) {
            if (chargedBytes > 0L) storageBudget.release(chargedBytes)
            chargedBytes = bytes
        }
        return true
    }

    fun popSkipped(token: Long): Boolean {
        if (token >= 0L || token != skippedToken || skippedDepth == 0L) return false
        if (!skippedOverflow) {
            skippedDepth--
            if (skippedDepth == 0L) skippedToken = 0L
        }
        return true
    }

    /** Returns false and discards unmatched inner frames when exits arrive out of LIFO order. */
    fun pop(methodId: Long): Boolean {
        clearPopped()
        if (skippedDepth > 0L) {
            reset()
            return false
        }
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
            shrinkEmptyOversizedStack()
            return true
        }
        for (index in depth - 2 downTo 0) {
            if (ids[index] == methodId) {
                clearRange(index, depth)
                depth = index
                shrinkEmptyOversizedStack()
                return false
            }
        }
        reset()
        return false
    }

    fun reset() {
        clearRange(0, depth)
        depth = 0
        skippedDepth = 0L
        skippedToken = 0L
        skippedOverflow = false
        clearPopped()
        shrinkEmptyOversizedStack()
    }

    fun releaseStorage() {
        if (released) return
        released = true
        reset()
        discardArrays()
    }

    private fun shrinkEmptyOversizedStack() {
        if (storageBudget != null && depth == 0 && ids.size > INITIAL_DEPTH) discardArrays()
    }

    private fun discardArrays() {
        ids = EMPTY_LONGS
        startedAtMs = EMPTY_LONGS
        names = EMPTY_NAMES
        screens = EMPTY_NAMES
        operationIds = EMPTY_LONGS
        if (chargedBytes > 0L) storageBudget?.release(chargedBytes)
        chargedBytes = 0L
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
        const val INITIAL_DEPTH = RuntimeGraphStorageBudget.INITIAL_STACK_DEPTH
        val EMPTY_LONGS = LongArray(0)
        val EMPTY_NAMES = emptyArray<String?>()
    }
}

internal class RuntimeGraphAggregateBuffer(
    thread: Thread,
    private val storageBudget: RuntimeGraphStorageBudget? = null,
) {
    val owner = WeakReference(thread)
    private val pages = arrayOfNulls<RuntimeGraphAggregatePage>(RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY)
    private val sequencer = SpscSlotSequencer(RUNTIME_GRAPH_PAGE_QUEUE_CAPACITY)
    private val publishedLogicalEvents = AtomicLong()
    private var producerPosition = SpscSlotSequencer.NO_POSITION
    private var producerPage: RuntimeGraphAggregatePage? = null

    @Volatile private var producerAttempted = 0L
    @Volatile private var producerAccepted = 0L

    @Volatile var producerWaiting = false
    @Volatile var producerActive = false
    // Stays true while a producer temporarily yields producerActive to a consumer page rotation.
    @Volatile var producerAdmitted = false

    @Volatile var rotationRequested = false

    // Producer-owned, shared by rotation and capacity waits in one admission; no per-event object.
    var admissionStartedAtNs = 0L
    var admissionWaitNs = 0L

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
        producerPage = null
        return true
    }

    fun tryClaimConsumer(): Long = sequencer.tryClaimConsumer()

    fun pageAt(position: Long): RuntimeGraphAggregatePage = checkNotNull(pages[sequencer.slotIndex(position)])

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
        pages.forEach { it?.clear() }
        publishedLogicalEvents.set(0L)
        producerPosition = SpscSlotSequencer.NO_POSITION
        producerPage = null
        producerWaiting = false
        producerActive = false
        producerAdmitted = false
        rotationRequested = false
        producerAttempted = 0L
        producerAccepted = 0L
    }

    /** Consumer owns all slots after producer quiescence. Detached pages carry no labels. */
    fun releaseStorage() {
        clear()
        for (index in pages.indices) {
            val page = pages[index] ?: continue
            pages[index] = null
            storageBudget?.recyclePage(page)
        }
    }

    /** Called inside the existing rotation handshake, never alongside a producer mutation. */
    fun trimEmptyPages() {
        if (storageBudget == null) return
        for (index in pages.indices) {
            val page = pages[index] ?: continue
            if (page === producerPage || page.size != 0) continue
            pages[index] = null
            storageBudget.recyclePage(page)
        }
    }

    private fun ensureActivePage(): Boolean {
        if (producerPosition != SpscSlotSequencer.NO_POSITION) return true
        val position = sequencer.tryClaimProducer()
        if (position == SpscSlotSequencer.NO_POSITION) return false
        val slot = sequencer.slotIndex(position)
        // This unpublished slot belongs exclusively to its producer; release/acquire publication
        // makes both the page reference and its payload visible to the consumer.
        if (pages[slot] == null) {
            pages[slot] = if (storageBudget == null) RuntimeGraphAggregatePage()
                else storageBudget.acquirePage() ?: return false
        }
        producerPosition = position
        producerPage = pages[slot]
        return true
    }

    private fun activePage(): RuntimeGraphAggregatePage {
        return checkNotNull(producerPage) { "Runtime graph producer page is not claimed" }
    }
}

internal class RuntimeGraphAggregatePage {
    // Only a detached, empty page may participate in its session's bounded recycle list.
    var recycledNext: RuntimeGraphAggregatePage? = null
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
        val newCapacity = if ((size + 1) * LOAD_DENOMINATOR < states.size * LOAD_NUMERATOR) {
            states.size
        } else {
            states.size shl 1
        }
        rehash(newCapacity)
    }

    private fun rehash(capacity: Int) {
        val snapshot = StorageSnapshot(
            states, hashes, callers, callerNames, callees, calleeNames, counts, totalsMs, maximaMs,
            screens, operationIds,
        )
        allocate(capacity)
        for (source in snapshot.states.indices) {
            if (snapshot.states[source] != OCCUPIED) continue
            val target = findInsertIndex(snapshot.hashes[source])
            copyFromSnapshot(snapshot, source, target)
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

    private fun copyFromSnapshot(snapshot: StorageSnapshot, source: Int, target: Int) {
        states[target] = OCCUPIED
        hashes[target] = snapshot.hashes[source]
        callers[target] = snapshot.callers[source]
        callerNames[target] = snapshot.callerNames[source]
        callees[target] = snapshot.callees[source]
        calleeNames[target] = snapshot.calleeNames[source]
        counts[target] = snapshot.counts[source]
        totalsMs[target] = snapshot.totalsMs[source]
        maximaMs[target] = snapshot.maximaMs[source]
        screens[target] = snapshot.screens[source]
        operationIds[target] = snapshot.operationIds[source]
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

private const val HASH_CALLEE_ROTATION = 29
private const val HASH_SCREEN_SHIFT = 32
private const val HASH_OPERATION_ROTATION = 17
private const val HASH_AVALANCHE_SHIFT = 33
private const val HASH_FOLD_SHIFT = 32
private const val HASH_AVALANCHE_MULTIPLIER_1 = -49064778989728563L
private const val HASH_AVALANCHE_MULTIPLIER_2 = -4265267296055464877L

private fun runtimeGraphEdgeHash(caller: Long, callee: Long, screen: String?, operationId: Long): Int {
    var mixed = caller xor java.lang.Long.rotateLeft(callee, HASH_CALLEE_ROTATION)
    mixed = mixed xor ((screen?.hashCode() ?: 0).toLong() shl HASH_SCREEN_SHIFT)
    mixed = mixed xor java.lang.Long.rotateLeft(operationId, HASH_OPERATION_ROTATION)
    mixed = (mixed xor (mixed ushr HASH_AVALANCHE_SHIFT)) * HASH_AVALANCHE_MULTIPLIER_1
    mixed = (mixed xor (mixed ushr HASH_AVALANCHE_SHIFT)) * HASH_AVALANCHE_MULTIPLIER_2
    return (mixed xor (mixed ushr HASH_FOLD_SHIFT)).toInt()
}
