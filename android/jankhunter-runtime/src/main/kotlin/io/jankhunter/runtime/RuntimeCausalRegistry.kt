package io.jankhunter.runtime

import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantLock

/** Bounded epoch-owned weak identity table. Live entries are never evicted or held strongly. */
internal class RuntimeCausalRegistry(
    capacity: Int,
    private val hash: (Any) -> Int = System::identityHashCode,
    private val nextId: AtomicLong = AtomicLong(),
) {
    private val shards: Array<Shard>

    @Volatile private var closed = false

    init {
        require(capacity > 0)
        val shardCount = minOf(MAX_SHARDS, maxOf(1, capacity / MIN_SHARD_CAPACITY))
        shards = Array(shardCount) { index ->
            val shardCapacity = capacity / shardCount + if (index < capacity % shardCount) 1 else 0
            Shard(shardCapacity)
        }
    }

    /** For entities whose handle is constructed before it can be weakly bound. Gaps are allowed. */
    fun newId(): Long {
        if (closed) return 0L
        return nextId.incrementAndGet().takeIf { it > 0L && !closed } ?: 0L
    }

    fun register(kind: Int, identity: Any): Long {
        if (closed) return 0L
        val identityHash = hash(identity)
        val shard = shard(identityHash)
        return withShard(shard, 0L) {
            if (closed) return 0L
            shard.drain()
            shard.find(kind, identity, identityHash)?.let { return it.id }
            if (shard.live >= shard.capacity) return 0L
            val id = newId().takeIf { it > 0L } ?: return 0L
            shard.insert(kind, identity, identityHash, id)
            return id
        }
    }

    /** Binds another object (for example a returned Future) to an existing entity ID. */
    fun bind(kind: Int, identity: Any, id: Long): Boolean {
        if (closed || id <= 0L) return false
        val identityHash = hash(identity)
        val shard = shard(identityHash)
        return withShard(shard, false) {
            if (closed) return false
            shard.drain()
            val existing = shard.find(kind, identity, identityHash)
            if (existing != null) return existing.id == id
            if (shard.live >= shard.capacity) return false
            shard.insert(kind, identity, identityHash, id)
            return true
        }
    }

    fun find(kind: Int, identity: Any): Long {
        if (closed) return 0L
        val identityHash = hash(identity)
        val shard = shard(identityHash)
        return withShard(shard, 0L) {
            if (closed) return 0L
            shard.drain()
            return shard.find(kind, identity, identityHash)?.id ?: 0L
        }
    }

    /** A root run is attributable only after this exact Thread received a start call. */
    fun markThreadStart(thread: Thread): Boolean {
        val identityHash = hash(thread)
        val shard = shard(identityHash)
        return withShard(shard, false) {
            if (closed) return false
            shard.drain()
            val entry = shard.find(1, thread, identityHash) ?: return false
            if (entry.pendingStarts < Int.MAX_VALUE) entry.pendingStarts++
            return true
        }
    }

    fun markThreadStartResult(thread: Thread, returned: Boolean) {
        val identityHash = hash(thread)
        val shard = shard(identityHash)
        return withShard(shard, Unit) {
            if (closed) return
            shard.drain()
            val entry = shard.find(1, thread, identityHash) ?: return
            if (entry.pendingStarts > 0) entry.pendingStarts--
            if (returned) entry.startedSuccessfully = true
        }
    }

    fun hasThreadStart(thread: Thread): Boolean {
        val identityHash = hash(thread)
        val shard = shard(identityHash)
        return withShard(shard, false) {
            if (closed) return false
            shard.drain()
            val entry = shard.find(1, thread, identityHash) ?: return false
            return entry.pendingStarts > 0 || entry.startedSuccessfully
        }
    }

    /** Returns the existing exact Thread ID only on its first observed terminal-state check. */
    fun markThreadEndObserved(thread: Thread): Long {
        val identityHash = hash(thread)
        val shard = shard(identityHash)
        return withShard(shard, 0L) {
            if (closed) return 0L
            shard.drain()
            val entry = shard.find(1, thread, identityHash) ?: return 0L
            if (entry.endObserved) return 0L
            entry.endObserved = true
            return entry.id
        }
    }

    fun release(kind: Int, identity: Any): Boolean {
        if (closed) return false
        val identityHash = hash(identity)
        val shard = shard(identityHash)
        return withShard(shard, false) {
            if (closed) return false
            shard.drain()
            return shard.remove(kind, identity, identityHash)
        }
    }

    fun liveEntries(): Int = shards.sumOf { shard -> withShard(shard, 0) { shard.drain(); shard.live } }

    internal fun allocatedSlotsForTest(): Int = shards.sumOf { it.slotCount }

    fun close() {
        closed = true
        shards.forEach { shard -> withShard(shard, Unit) { shard.clear() } }
    }

    internal fun clearAndEnqueueForTest(kind: Int, identity: Any): Boolean {
        val identityHash = hash(identity)
        val shard = shard(identityHash)
        return withShard(shard, false) {
            val entry = shard.find(kind, identity, identityHash) ?: return false
            entry.clear()
            entry.enqueue()
            return true
        }
    }

    private inline fun <T> withShard(shard: Shard, fallback: T, block: () -> T): T {
        if (!shard.lock.tryLock()) {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.REGISTRY_CONTENTION)
            return fallback
        }
        try { return block() }
        finally {
            // close may lose tryLock to this owner. Clearing under the owned lock prevents
            // deferred weak entries from surviving a closed epoch.
            if (closed) shard.clear()
            shard.lock.unlock()
        }
    }

    private fun shard(identityHash: Int): Shard {
        val mixed = identityHash xor (identityHash ushr 16)
        return shards[(mixed and Int.MAX_VALUE) % shards.size]
    }

    private class Entry(
        target: Any,
        queue: ReferenceQueue<Any>,
        val kind: Int,
        val hash: Int,
        val id: Long,
        var slot: Int,
    ) : WeakReference<Any>(target, queue) {
        var pendingStarts: Int = 0
        var startedSuccessfully: Boolean = false
        var endObserved: Boolean = false
    }

    private class Shard(val capacity: Int) {
        val lock = ReentrantLock()
        private val queue = ReferenceQueue<Any>()
        private var cells = arrayOfNulls<Any>(tableSize(capacity))
        val slotCount: Int get() = cells.size
        var live = 0
            private set
        private var tombstones = 0

        fun find(kind: Int, identity: Any, hash: Int): Entry? {
            var slot = hash and (cells.size - 1)
            repeat(cells.size) {
                when (val cell = cells[slot]) {
                    null -> return null
                    TOMBSTONE -> Unit
                    else -> {
                        val entry = cell as Entry
                        val target = entry.get()
                        if (target == null) delete(slot)
                        else if (entry.hash == hash && entry.kind == kind && target === identity) return entry
                    }
                }
                slot = (slot + 1) and (cells.size - 1)
            }
            return null
        }

        fun insert(kind: Int, identity: Any, hash: Int, id: Long) {
            var slot = hash and (cells.size - 1)
            var available = -1
            repeat(cells.size) {
                when (cells[slot]) {
                    null -> {
                        val target = if (available >= 0) available else slot
                        put(target, kind, identity, hash, id)
                        return
                    }
                    TOMBSTONE -> if (available < 0) available = slot
                }
                slot = (slot + 1) and (cells.size - 1)
            }
            check(available >= 0)
            put(available, kind, identity, hash, id)
        }

        fun remove(kind: Int, identity: Any, hash: Int): Boolean {
            val entry = find(kind, identity, hash) ?: return false
            delete(entry.slot)
            return true
        }

        fun drain() {
            repeat(16) {
                val entry = queue.poll() as? Entry ?: return
                if (entry.slot in cells.indices && cells[entry.slot] === entry) delete(entry.slot)
            }
        }

        fun clear() {
            cells.fill(null)
            while (queue.poll() != null) Unit
            live = 0
            tombstones = 0
        }

        private fun put(slot: Int, kind: Int, identity: Any, hash: Int, id: Long) {
            if (cells[slot] === TOMBSTONE) tombstones--
            cells[slot] = Entry(identity, queue, kind, hash, id, slot)
            live++
        }

        private fun delete(slot: Int) {
            if (cells[slot] !is Entry) return
            cells[slot] = TOMBSTONE
            live--
            tombstones++
        }

        private companion object {
            val TOMBSTONE = Any()

            fun tableSize(capacity: Int): Int {
                var result = 2
                val required = capacity.toLong() * 2L
                while (result.toLong() < required) result = result shl 1
                return result
            }
        }
    }

    private companion object {
        const val MAX_SHARDS = 16
        const val MIN_SHARD_CAPACITY = 16
    }
}
