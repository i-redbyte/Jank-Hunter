package io.jankhunter.runtime

import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

internal class HandlerWrapperRegistry(
    private val droppedCounter: (HandlerWrapperLoss) -> Unit,
    private val exactAdmission: RuntimeBooleanSource = RuntimeBooleanSource { false },
) {
    private val shards = Array(SHARD_COUNT) { Shard() }
    private val entryCount = AtomicInteger()

    fun register(
        handler: Any,
        runnable: Runnable,
        token: Any?,
        wrapper: Runnable,
        maxEntries: Int,
        maxWrappers: Int,
    ): Boolean {
        val shard = shardFor(handler)
        val exact = exactAdmission.getAsBoolean()
        if (!acquire(shard, exact)) return false
        try {
            cleanLocked(shard)
            var entry = findEntryLocked(shard, handler, runnable)
            if (entry != null) {
                cleanEntryLocked(shard, entry)
                if (!shard.entriesByKey.containsKey(entry.key)) {
                    entry = null
                }
            }
            if (maxWrappers <= 0) {
                droppedCounter(HandlerWrapperLoss.WRAPPER_LIMIT)
                return false
            }
            var entryReserved = false
            if (entry == null) {
                entryReserved = tryReserveEntry(maxEntries)
            }
            if (entry == null && !entryReserved) {
                droppedCounter(HandlerWrapperLoss.ENTRY_LIMIT)
                return false
            }
            val resolvedEntry = entry ?: try {
                createEntryLocked(shard, handler, runnable)
            } catch (throwable: Throwable) {
                entryCount.decrementAndGet()
                throw throwable
            }
            if (resolvedEntry.wrappers.size >= maxWrappers) {
                droppedCounter(HandlerWrapperLoss.WRAPPER_LIMIT)
                return false
            }
            resolvedEntry.wrappers.add(
                WrapperEntry(
                    wrapper = WeakReference(wrapper),
                    token = token?.let(::WeakReference),
                ),
            )
            shard.dirty = true
            return true
        } finally {
            publishSnapshot(shard)
            shard.lock.unlock()
        }
    }

    fun wrappers(handler: Any, runnable: Runnable, token: Any?): Array<Runnable> {
        val shard = shardFor(handler)
        for (entry in shard.snapshot) {
            if (entry.key.handler() !== handler || entry.key.original() !== runnable) continue
            val candidates = entry.wrappers
            val result = Array(candidates.size) { EMPTY_RUNNABLE }
            var size = 0
            for (candidate in candidates) {
                if (!tokenMatches(candidate.token, token)) continue
                val wrapper = candidate.wrapper.get() ?: continue
                result[size++] = wrapper
            }
            return if (size == result.size) {
                result
            } else {
                result.copyOfRange(0, size)
            }
        }
        return EMPTY_WRAPPERS
    }

    fun unregister(delegate: Runnable, wrapper: Runnable) {
        val originalHash = System.identityHashCode(delegate)
        val exact = exactAdmission.getAsBoolean()
        shards.forEach { shard ->
            if (!acquire(shard, exact)) return@forEach
            try {
                cleanLocked(shard)
                val keys = shard.keysByOriginalHash[originalHash]?.toList().orEmpty()
                keys.forEach { key ->
                    val entry = shard.entriesByKey[key] ?: return@forEach
                    if (key.original() === delegate) {
                        if (removeWrappers(entry.wrappers) {
                            val candidate = it.wrapper.get()
                            candidate == null || candidate === wrapper
                        }) shard.dirty = true
                        if (entry.wrappers.isEmpty()) {
                            removeEntryLocked(shard, key)
                        }
                    } else if (key.isCleared()) {
                        removeEntryLocked(shard, key)
                    }
                }
            } finally {
                publishSnapshot(shard)
                shard.lock.unlock()
            }
        }
    }

    fun unregister(handler: Any, runnable: Runnable, token: Any?) {
        val shard = shardFor(handler)
        if (!containsEntry(shard.snapshot, handler, runnable)) return
        if (!acquire(shard, exactAdmission.getAsBoolean())) return
        try {
            cleanLocked(shard)
            val entry = findEntryLocked(shard, handler, runnable) ?: return
            if (removeWrappers(entry.wrappers) { it.wrapper.get() == null || tokenMatches(it.token, token) }) {
                shard.dirty = true
            }
            if (entry.wrappers.isEmpty()) {
                removeEntryLocked(shard, entry.key)
            }
        } finally {
            publishSnapshot(shard)
            shard.lock.unlock()
        }
    }

    fun unregister(handler: Any, token: Any?) {
        val shard = shardFor(handler)
        if (!acquire(shard, exactAdmission.getAsBoolean())) return
        try {
            cleanLocked(shard)
            val keys = shard.keysByHandlerHash[System.identityHashCode(handler)]?.toList() ?: return
            keys.forEach { key ->
                val entry = shard.entriesByKey[key] ?: return@forEach
                if (key.handler() === handler) {
                    if (removeWrappers(entry.wrappers) { it.wrapper.get() == null || tokenMatches(it.token, token) }) {
                        shard.dirty = true
                    }
                    if (entry.wrappers.isEmpty()) {
                        removeEntryLocked(shard, key)
                    }
                } else if (key.isCleared()) {
                    removeEntryLocked(shard, key)
                }
            }
        } finally {
            publishSnapshot(shard)
            shard.lock.unlock()
        }
    }

    fun clear() {
        shards.forEach { shard ->
            shard.lock.withLock {
                shard.entriesByKey.clear()
                shard.keysByHandlerHash.clear()
                shard.keysByOriginalHash.clear()
                while (shard.referenceQueue.poll() != null) Unit
                shard.dirty = true
                publishSnapshot(shard)
            }
        }
        entryCount.set(0)
    }

    private fun cleanLocked(shard: Shard) {
        while (true) {
            val reference = shard.referenceQueue.poll() as? EntryReference<*> ?: break
            removeEntryLocked(shard, reference.key)
        }
    }

    private fun cleanEntryLocked(shard: Shard, entry: Entry) {
        if (removeWrappers(entry.wrappers) { it.wrapper.get() == null }) shard.dirty = true
        if (entry.wrappers.isEmpty()) {
            removeEntryLocked(shard, entry.key)
        }
    }

    private fun publishSnapshot(shard: Shard) {
        if (!shard.dirty) return
        val entries = shard.entriesByKey.values
        val iterator = entries.iterator()
        shard.snapshot = Array(entries.size) {
            val entry = iterator.next()
            SnapshotEntry(entry.key, entry.wrappers.toTypedArray())
        }
        shard.dirty = false
    }

    private fun createEntryLocked(shard: Shard, handler: Any, runnable: Runnable): Entry {
        val key = EntryKey(handler, runnable, shard.referenceQueue)
        val entry = Entry(key, mutableListOf())
        shard.entriesByKey[key] = entry
        shard.keysByHandlerHash.getOrPut(key.handlerHash) { mutableSetOf() }.add(key)
        shard.keysByOriginalHash.getOrPut(key.originalHash) { mutableSetOf() }.add(key)
        shard.dirty = true
        return entry
    }

    private fun findEntryLocked(shard: Shard, handler: Any, runnable: Runnable): Entry? {
        val handlerKeys = shard.keysByHandlerHash[System.identityHashCode(handler)] ?: return null
        val originalKeys = shard.keysByOriginalHash[System.identityHashCode(runnable)] ?: return null
        val candidates = if (handlerKeys.size <= originalKeys.size) handlerKeys else originalKeys
        for (key in candidates) {
            if (key.handler() === handler && key.original() === runnable) {
                return shard.entriesByKey[key]
            }
        }
        return null
    }

    private fun containsEntry(snapshot: Array<SnapshotEntry>, handler: Any, runnable: Runnable): Boolean {
        for (entry in snapshot) {
            if (entry.key.handler() === handler && entry.key.original() === runnable) return true
        }
        return false
    }

    private inline fun removeWrappers(
        wrappers: MutableList<WrapperEntry>,
        shouldRemove: (WrapperEntry) -> Boolean,
    ): Boolean {
        var changed = false
        for (index in wrappers.lastIndex downTo 0) {
            if (shouldRemove(wrappers[index])) {
                wrappers.removeAt(index)
                changed = true
            }
        }
        return changed
    }

    private fun removeEntryLocked(shard: Shard, key: EntryKey) {
        if (shard.entriesByKey.remove(key) == null) return
        shard.dirty = true
        entryCount.decrementAndGet()
        shard.keysByHandlerHash[key.handlerHash]?.let { keys ->
            keys.remove(key)
            if (keys.isEmpty()) {
                shard.keysByHandlerHash.remove(key.handlerHash)
            }
        }
        shard.keysByOriginalHash[key.originalHash]?.let { keys ->
            keys.remove(key)
            if (keys.isEmpty()) {
                shard.keysByOriginalHash.remove(key.originalHash)
            }
        }
    }

    private fun tryReserveEntry(maxEntries: Int): Boolean {
        if (maxEntries <= 0) return false
        while (true) {
            val current = entryCount.get()
            if (current >= maxEntries) return false
            if (entryCount.compareAndSet(current, current + 1)) return true
        }
    }

    private fun acquire(shard: Shard, exact: Boolean): Boolean {
        if (exact) {
            shard.lock.lock()
            return true
        }
        if (shard.lock.tryLock()) return true
        droppedCounter(HandlerWrapperLoss.CONTENTION)
        return false
    }

    private fun shardFor(handler: Any): Shard = shards[shardIndex(System.identityHashCode(handler))]

    private fun shardIndex(hash: Int): Int {
        val mixed = hash xor (hash ushr 16)
        return mixed and (SHARD_COUNT - 1)
    }

    private fun tokenMatches(registeredToken: WeakReference<Any>?, requestedToken: Any?): Boolean {
        return requestedToken == null || registeredToken?.get() === requestedToken
    }

    private class Shard {
        val lock = ReentrantLock()
        val referenceQueue = ReferenceQueue<Any>()
        val entriesByKey = HashMap<EntryKey, Entry>()
        val keysByHandlerHash = HashMap<Int, MutableSet<EntryKey>>()
        val keysByOriginalHash = HashMap<Int, MutableSet<EntryKey>>()
        var dirty = false

        @Volatile
        var snapshot = emptyArray<SnapshotEntry>()
    }

    private class EntryKey(
        handler: Any,
        original: Runnable,
        referenceQueue: ReferenceQueue<Any>,
    ) {
        val handlerHash: Int = System.identityHashCode(handler)
        val originalHash: Int = System.identityHashCode(original)
        private val handlerRef = EntryReference(handler, referenceQueue, this)
        private val originalRef = EntryReference(original, referenceQueue, this)

        fun handler(): Any? = handlerRef.get()

        fun original(): Runnable? = originalRef.get()

        fun isCleared(): Boolean = handler() == null || original() == null

        override fun equals(other: Any?): Boolean {
            if (this === other) return true
            if (other !is EntryKey) return false
            val currentHandler = handler() ?: return false
            val currentOriginal = original() ?: return false
            return handlerHash == other.handlerHash &&
                originalHash == other.originalHash &&
                currentHandler === other.handler() &&
                currentOriginal === other.original()
        }

        override fun hashCode(): Int = 31 * handlerHash + originalHash
    }

    private class EntryReference<T : Any>(
        referent: T,
        queue: ReferenceQueue<Any>,
        val key: EntryKey,
    ) : WeakReference<T>(referent, queue)

    private class Entry(
        val key: EntryKey,
        val wrappers: MutableList<WrapperEntry>,
    )

    private class WrapperEntry(
        val wrapper: WeakReference<Runnable>,
        val token: WeakReference<Any>?,
    )

    private class SnapshotEntry(
        val key: EntryKey,
        val wrappers: Array<WrapperEntry>,
    )

    private companion object {
        const val SHARD_COUNT = 16
        val EMPTY_WRAPPERS = emptyArray<Runnable>()
        val EMPTY_RUNNABLE = Runnable {}
    }
}

internal enum class HandlerWrapperLoss {
    ENTRY_LIMIT,
    WRAPPER_LIMIT,
    CONTENTION,
}
