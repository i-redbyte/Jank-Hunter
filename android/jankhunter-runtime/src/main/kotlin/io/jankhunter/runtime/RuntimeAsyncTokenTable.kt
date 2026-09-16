package io.jankhunter.runtime

import java.util.concurrent.atomic.AtomicLong

/** Monotonic IDs are never recycled, including across collection epochs. */
internal class RuntimeAsyncTokenIds(initialSequence: Long = 0L) {
    private val sequence = AtomicLong(initialSequence)

    fun current(): Long = sequence.get()

    fun next(): Long {
        while (true) {
            val current = sequence.get()
            if (current >= MAX_SEQUENCE) return 0L
            if (sequence.compareAndSet(current, current + 1L)) return current + 1L
        }
    }

    companion object {
        const val MAX_SEQUENCE = Long.MAX_VALUE ushr RuntimeAsyncTokenTable.TOKEN_SEQUENCE_SHIFT
    }
}

/**
 * Direct-addressed slots with a free list: O(1) claim/release and amortized O(1) begin, without hashing or boxed keys.
 * Pages are allocated only as concurrent work grows. A token carries a unique serial, slot and kind;
 * reusing a slot cannot make an earlier completion valid. Timestamps retain all 64 bits separately.
 * Claimed slots remain occupied until publication returns, so close distinguishes unfinished work
 * from completions already in progress. No application object or writer is retained here.
 */
internal class RuntimeAsyncTokenTable(
    private val ids: RuntimeAsyncTokenIds,
    private val capacity: Int = MAX_CAPACITY,
    private val rejected: (Int) -> Unit = {},
) {
    private val firstSequence = ids.current() + 1L
    private val pages = ArrayList<LongArray>()
    private val active = LongArray(KIND_COUNT)
    private val publishing = LongArray(KIND_COUNT)
    private var nextUnused = 0
    private var freeHead = -1
    private var closed = false

    init { require(capacity in 1..MAX_CAPACITY) }

    @Synchronized
    fun begin(kind: Int, startedNanos: Long): Long {
        require(kind in HTTP..WORKER)
        require(startedNanos >= 0L)
        if (closed) return reject(REJECT_CLOSED)
        if (freeHead < 0 && nextUnused >= capacity) return reject(REJECT_CAPACITY)
        val serial = ids.next()
        if (serial == 0L) return reject(REJECT_ID_EXHAUSTED)
        val slot = if (freeHead >= 0) {
            freeHead.also { freeHead = time(it).toInt() }
        } else {
            val index = nextUnused
            if (index / PAGE_SIZE == pages.size) pages.add(LongArray(PAGE_SIZE * WORDS_PER_SLOT))
            nextUnused++
            index
        }
        val token = (serial shl TOKEN_SEQUENCE_SHIFT) or (slot.toLong() shl KIND_BITS) or kind.toLong()
        set(slot, token, startedNanos)
        active[kind]++
        return token
    }

    /** Transaction stacks already own exact active identities and consume terminal callbacks once.
     * Their storage grows with active depth; they are not subject to the async slot capacity. */
    @Synchronized
    fun beginTransaction(): Long {
        if (closed) return reject(REJECT_CLOSED)
        val serial = ids.next()
        if (serial == 0L) return reject(REJECT_ID_EXHAUSTED)
        active[DATABASE_TRANSACTION]++
        // The transaction stack already identifies the kind; keep wire IDs and their deltas compact.
        return serial
    }

    @Synchronized
    fun claimTransaction(transactionId: Long): Boolean {
        val reason = when {
            closed || transactionId < firstSequence -> REJECT_CLOSED
            transactionId <= 0L || transactionId > ids.current() -> REJECT_INVALID
            active[DATABASE_TRANSACTION] == 0L -> REJECT_CONSUMED
            else -> 0
        }
        if (reason != 0) {
            reject(reason)
            return false
        }
        active[DATABASE_TRANSACTION]--
        publishing[DATABASE_TRANSACTION]++
        return true
    }

    @Synchronized
    fun releaseTransaction() {
        if (!closed && publishing[DATABASE_TRANSACTION] > 0L) publishing[DATABASE_TRANSACTION]--
    }

    /** Returns the exact start time, or -1 when this completion must not be published. */
    @Synchronized
    fun claim(token: Long, kind: Int): Long {
        if (token <= 0L || kind !in 1 until KIND_COUNT || token and KIND_MASK != kind.toLong()) {
            reject(REJECT_INVALID)
            return -1L
        }
        val slot = slot(token)
        val serial = token ushr TOKEN_SEQUENCE_SHIFT
        val reason = when {
            closed || serial < firstSequence -> REJECT_CLOSED
            serial > ids.current() -> REJECT_INVALID
            slot >= nextUnused || identity(slot) != token -> REJECT_CONSUMED
            else -> 0
        }
        if (reason != 0) {
            reject(reason)
            return -1L
        }
        val started = time(slot)
        set(slot, -token, started)
        active[kind]--
        publishing[kind]++
        return started
    }

    @Synchronized
    fun release(token: Long) {
        if (closed || token <= 0L) return
        val slot = slot(token)
        if (slot >= nextUnused || identity(slot) != -token) return
        publishing[(token and KIND_MASK).toInt()]--
        set(slot, 0L, freeHead.toLong())
        freeHead = slot
    }

    /** The callback runs once at close; it must only record the primitive quality counters. */
    @Synchronized
    fun close(record: (kind: Int, unfinished: Long, completing: Long) -> Unit) {
        if (closed) return
        closed = true
        for (kind in 1 until KIND_COUNT) record(kind, active[kind], publishing[kind])
        pages.clear()
        active.fill(0L)
        publishing.fill(0L)
        nextUnused = 0
        freeHead = -1
    }

    private fun reject(reason: Int): Long {
        rejected(reason)
        return 0L
    }

    private fun slot(token: Long): Int = ((token ushr KIND_BITS) and SLOT_MASK).toInt()

    private fun identity(slot: Int): Long = pages[slot / PAGE_SIZE][(slot % PAGE_SIZE) * WORDS_PER_SLOT]

    private fun time(slot: Int): Long = pages[slot / PAGE_SIZE][(slot % PAGE_SIZE) * WORDS_PER_SLOT + 1]

    private fun set(slot: Int, identity: Long, time: Long) {
        val page = pages[slot / PAGE_SIZE]
        val offset = (slot % PAGE_SIZE) * WORDS_PER_SLOT
        page[offset] = identity
        page[offset + 1] = time
    }

    companion object {
        const val HTTP = 1
        const val DATABASE = 2
        const val WORKER = 3
        const val DATABASE_TRANSACTION = 4
        const val KIND_COUNT = 5
        const val REJECT_CLOSED = 1
        const val REJECT_CONSUMED = 2
        const val REJECT_INVALID = 3
        const val REJECT_CAPACITY = 4
        const val REJECT_ID_EXHAUSTED = 5
        const val TOKEN_SEQUENCE_SHIFT = 18
        private const val KIND_BITS = 2
        private const val KIND_MASK = 3L
        private const val SLOT_MASK = 65_535L
        private const val MAX_CAPACITY = 65_536
        private const val PAGE_SIZE = 64
        private const val WORDS_PER_SLOT = 2
    }
}
