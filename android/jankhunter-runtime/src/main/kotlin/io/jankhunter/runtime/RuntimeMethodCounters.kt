package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.QualityCounterId
import io.jankhunter.runtime.internal.io.StableCounterBatch
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong

/** Primitive, sharded aggregation for ASM method counters keyed by stable 64-bit IDs. */
internal class RuntimeMethodCounters(
    private val nowMs: () -> Long,
    private val maxKeys: () -> Int,
) {
    private val shards = Array(SHARD_COUNT) { CounterShard() }
    private val entryCount = AtomicInteger()
    private val epoch = AtomicLong(1L)
    private val capacityLoss = AtomicLong()
    private val lastFlushAtMs = AtomicLong()
    private val flushLock = Any()
    private var nextFlushShard = 0

    fun resetFlushState() {
        advanceEpoch()
        lastFlushAtMs.set(nowMs())
        capacityLoss.set(0L)
    }

    fun clear() {
        advanceEpoch()
        shards.forEach { shard ->
            synchronized(shard.lock) { shard.clear() }
        }
        entryCount.set(0)
        lastFlushAtMs.set(0L)
        capacityLoss.set(0L)
    }

    fun record(methodId: Long, enabled: Boolean, writer: AsyncLogWriter?) {
        record(methodId, null, enabled, writer)
    }

    fun record(methodId: Long, methodName: String?, enabled: Boolean, writer: AsyncLogWriter?) {
        if (!enabled || writer == null) return
        val expectedEpoch = epoch.get()
        val hash = idHash(methodId)
        val shard = shards[hash and (SHARD_COUNT - 1)]
        val checkClock = synchronized(shard.lock) {
            if (epoch.get() != expectedEpoch) return
            val existing = shard.find(methodId, hash)
            if (existing >= 0) {
                shard.increment(existing)
            } else {
                val limit = maxKeys().coerceAtLeast(0)
                if (!reserveEntry(limit)) {
                    capacityLoss.incrementAndGet()
                } else {
                    try {
                        shard.insert(methodId, methodName, hash)
                    } catch (throwable: Throwable) {
                        entryCount.decrementAndGet()
                        capacityLoss.incrementAndGet()
                        throw throwable
                    }
                }
            }
            shard.shouldCheckClock()
        }
        if (checkClock) maybeFlush(nowMs(), writer)
    }

    fun flush(force: Boolean, writer: AsyncLogWriter?) {
        val asyncWriter = writer ?: return
        flushAt(nowMs(), force, asyncWriter)
    }

    fun flushForShutdown(writer: AsyncLogWriter?) {
        advanceEpoch()
        val asyncWriter = writer ?: return
        synchronized(flushLock) {
            while (true) {
                val batch = takeBatch() ?: break
                asyncWriter.stableCounters(batch)
            }
            flushQuality(asyncWriter)
            lastFlushAtMs.set(nowMs())
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

    private fun advanceEpoch() {
        while (true) {
            val current = epoch.get()
            val next = if (current == Long.MAX_VALUE) 1L else current + 1L
            if (epoch.compareAndSet(current, next)) return
        }
    }

    private fun maybeFlush(now: Long, writer: AsyncLogWriter) {
        val last = lastFlushAtMs.get()
        if (now - last < FLUSH_INTERVAL_MS) return
        if (!lastFlushAtMs.compareAndSet(last, now)) return
        flushAt(now, force = true, writer)
    }

    private fun flushAt(now: Long, force: Boolean, writer: AsyncLogWriter) {
        if (!force) {
            val last = lastFlushAtMs.get()
            if (now - last < FLUSH_INTERVAL_MS) return
            if (!lastFlushAtMs.compareAndSet(last, now)) return
        }
        synchronized(flushLock) {
            takeBatch()?.let(writer::stableCounters)
            flushQuality(writer)
            val nextDelay = if (entryCount.get() > 0) DRAIN_INTERVAL_MS else FLUSH_INTERVAL_MS
            lastFlushAtMs.set(now - (FLUSH_INTERVAL_MS - nextDelay))
        }
    }

    private fun takeBatch(): StableCounterBatch? {
        if (entryCount.get() <= 0) return null
        val batch = StableCounterBatch(MAX_FLUSH_RECORDS)
        var visited = 0
        while (visited < SHARD_COUNT && batch.size < MAX_FLUSH_RECORDS) {
            val shardIndex = nextFlushShard
            nextFlushShard = (nextFlushShard + 1) and (SHARD_COUNT - 1)
            val shard = shards[shardIndex]
            val removed = synchronized(shard.lock) { shard.drainInto(batch, MAX_FLUSH_RECORDS) }
            if (removed > 0) entryCount.addAndGet(-removed)
            visited++
        }
        return batch.takeIf { it.size > 0 }
    }

    private fun flushQuality(writer: AsyncLogWriter) {
        val lost = capacityLoss.getAndSet(0L)
        if (lost > 0L) writer.recordQuality(QualityCounterId.METRIC_CARDINALITY_LOSS, lost)
    }

    private class CounterShard {
        val lock = Any()
        private var states = ByteArray(INITIAL_CAPACITY)
        private var hashes = IntArray(INITIAL_CAPACITY)
        private var ids = LongArray(INITIAL_CAPACITY)
        private var names = arrayOfNulls<String>(INITIAL_CAPACITY)
        private var counts = LongArray(INITIAL_CAPACITY)
        private var size = 0
        private var used = 0
        private var drainCursor = 0
        private var operationsUntilClock = CLOCK_CHECK_EVERY

        fun find(id: Long, hash: Int): Int {
            var index = hash and (states.size - 1)
            repeat(states.size) {
                when (states[index]) {
                    EMPTY -> return -1
                    OCCUPIED -> if (hashes[index] == hash && ids[index] == id) return index
                }
                index = (index + 1) and (states.size - 1)
            }
            return -1
        }

        fun increment(index: Int) {
            if (counts[index] < Long.MAX_VALUE) counts[index]++
        }

        fun insert(id: Long, name: String?, hash: Int) {
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
            ids[index] = id
            names[index] = name
            counts[index] = 1L
            size++
        }

        fun shouldCheckClock(): Boolean {
            operationsUntilClock--
            if (operationsUntilClock > 0) return false
            operationsUntilClock = CLOCK_CHECK_EVERY
            return true
        }

        fun drainInto(batch: StableCounterBatch, limit: Int): Int {
            if (size <= 0 || batch.size >= limit) return 0
            var removed = 0
            var visited = 0
            var index = drainCursor and (states.size - 1)
            while (visited < states.size && batch.size < limit) {
                if (states[index] == OCCUPIED) {
                    batch.add(ids[index], names[index], counts[index])
                    states[index] = DELETED
                    names[index] = null
                    counts[index] = 0L
                    size--
                    removed++
                }
                index = (index + 1) and (states.size - 1)
                visited++
            }
            drainCursor = index
            if (size == 0) {
                states.fill(EMPTY)
                used = 0
                drainCursor = 0
            }
            return removed
        }

        fun clear() {
            states.fill(EMPTY)
            hashes.fill(0)
            ids.fill(0L)
            names.fill(null)
            counts.fill(0L)
            size = 0
            used = 0
            drainCursor = 0
            operationsUntilClock = CLOCK_CHECK_EVERY
        }

        private fun ensureInsertCapacity() {
            if ((used + 1) * LOAD_DENOMINATOR < states.size * LOAD_NUMERATOR) return
            rehash(if (size * 2 < used) states.size else states.size shl 1)
        }

        private fun rehash(capacity: Int) {
            val oldStates = states
            val oldHashes = hashes
            val oldIds = ids
            val oldNames = names
            val oldCounts = counts
            states = ByteArray(capacity)
            hashes = IntArray(capacity)
            ids = LongArray(capacity)
            names = arrayOfNulls(capacity)
            counts = LongArray(capacity)
            size = 0
            used = 0
            drainCursor = 0
            for (oldIndex in oldStates.indices) {
                if (oldStates[oldIndex] != OCCUPIED) continue
                var index = oldHashes[oldIndex] and (capacity - 1)
                while (states[index] == OCCUPIED) index = (index + 1) and (capacity - 1)
                states[index] = OCCUPIED
                hashes[index] = oldHashes[oldIndex]
                ids[index] = oldIds[oldIndex]
                names[index] = oldNames[oldIndex]
                counts[index] = oldCounts[oldIndex]
                size++
                used++
            }
        }
    }

    private companion object {
        const val SHARD_COUNT = 16
        const val INITIAL_CAPACITY = 16
        const val MAX_FLUSH_RECORDS = 128
        const val CLOCK_CHECK_EVERY = 256
        const val FLUSH_INTERVAL_MS = 5_000L
        const val DRAIN_INTERVAL_MS = 250L
        const val LOAD_NUMERATOR = 3
        const val LOAD_DENOMINATOR = 4
        const val EMPTY: Byte = 0
        const val OCCUPIED: Byte = 1
        const val DELETED: Byte = 2

        fun idHash(id: Long): Int {
            var mixed = id
            mixed = (mixed xor (mixed ushr 33)) * -49064778989728563L
            mixed = (mixed xor (mixed ushr 33)) * -4265267296055464877L
            return (mixed xor (mixed ushr 32)).toInt()
        }
    }
}
