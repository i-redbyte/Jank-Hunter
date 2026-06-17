package io.jankhunter.runtime

import android.os.Handler
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.atomic.AtomicInteger

internal class HandlerWrapperRegistry(
    private val droppedCounter: (String) -> Unit,
) {
    private val shards = Array(SHARD_COUNT) { Shard() }
    private val entryCount = AtomicInteger()

    fun register(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        wrapper: Runnable,
        maxEntries: Int,
        maxWrappers: Int,
    ): Boolean {
        val shard = shardFor(handler)
        synchronized(shard.lock) {
            cleanLocked(shard)
            val lookupKey = LookupKey(handler, runnable)
            var entry = shard.entriesByKey[lookupKey]
            if (entry != null) {
                cleanEntryLocked(shard, entry)
                if (!shard.entriesByKey.containsKey(entry.key)) {
                    entry = null
                }
            }
            if (maxWrappers <= 0) {
                droppedCounter("jankhunter.handler_wrapper.dropped_wrappers.count")
                return false
            }
            var entryReserved = false
            if (entry == null) {
                entryReserved = tryReserveEntry(maxEntries)
            }
            if (entry == null && !entryReserved) {
                droppedCounter("jankhunter.handler_wrapper.dropped_entries.count")
                return false
            }
            val resolvedEntry = entry ?: try {
                createEntryLocked(shard, handler, runnable)
            } catch (throwable: Throwable) {
                entryCount.decrementAndGet()
                throw throwable
            }
            if (resolvedEntry.wrappers.size >= maxWrappers) {
                droppedCounter("jankhunter.handler_wrapper.dropped_wrappers.count")
                return false
            }
            resolvedEntry.wrappers.add(
                WrapperEntry(
                    wrapper = WeakReference(wrapper),
                    token = token?.let(::WeakReference),
                ),
            )
            return true
        }
    }

    fun wrappers(handler: Handler, runnable: Runnable, token: Any?): List<Runnable> {
        val shard = shardFor(handler)
        synchronized(shard.lock) {
            cleanLocked(shard)
            val entry = shard.entriesByKey[LookupKey(handler, runnable)] ?: return emptyList()
            cleanEntryLocked(shard, entry)
            if (!shard.entriesByKey.containsKey(entry.key)) return emptyList()
            return entry.wrappers
                .filter { tokenMatches(it.token, token) }
                .mapNotNull { it.wrapper.get() }
        }
    }

    fun unregister(delegate: Runnable, wrapper: Runnable) {
        val originalHash = System.identityHashCode(delegate)
        shards.forEach { shard ->
            synchronized(shard.lock) {
                cleanLocked(shard)
                val keys = shard.keysByOriginalHash[originalHash]?.toList().orEmpty()
                keys.forEach { key ->
                    val entry = shard.entriesByKey[key] ?: return@forEach
                    if (key.original() === delegate) {
                        entry.wrappers.removeAll { it.wrapper.get() == null || it.wrapper.get() === wrapper }
                        if (entry.wrappers.isEmpty()) {
                            removeEntryLocked(shard, key)
                        }
                    } else if (key.isCleared()) {
                        removeEntryLocked(shard, key)
                    }
                }
            }
        }
    }

    fun unregister(handler: Handler, runnable: Runnable, token: Any?) {
        val shard = shardFor(handler)
        synchronized(shard.lock) {
            cleanLocked(shard)
            val entry = shard.entriesByKey[LookupKey(handler, runnable)] ?: return
            entry.wrappers.removeAll { it.wrapper.get() == null || tokenMatches(it.token, token) }
            if (entry.wrappers.isEmpty()) {
                removeEntryLocked(shard, entry.key)
            }
        }
    }

    fun unregister(handler: Handler, token: Any?) {
        val shard = shardFor(handler)
        synchronized(shard.lock) {
            cleanLocked(shard)
            val keys = shard.keysByHandlerHash[System.identityHashCode(handler)]?.toList() ?: return
            keys.forEach { key ->
                val entry = shard.entriesByKey[key] ?: return@forEach
                if (key.handler() === handler) {
                    entry.wrappers.removeAll { it.wrapper.get() == null || tokenMatches(it.token, token) }
                    if (entry.wrappers.isEmpty()) {
                        removeEntryLocked(shard, key)
                    }
                } else if (key.isCleared()) {
                    removeEntryLocked(shard, key)
                }
            }
        }
    }

    fun clear() {
        shards.forEach { shard ->
            synchronized(shard.lock) {
                shard.entriesByKey.clear()
                shard.keysByHandlerHash.clear()
                shard.keysByOriginalHash.clear()
                while (shard.referenceQueue.poll() != null) {
                    // Drain stale key references for the cleared registry.
                }
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
        entry.wrappers.removeAll { it.wrapper.get() == null }
        if (entry.wrappers.isEmpty()) {
            removeEntryLocked(shard, entry.key)
        }
    }

    private fun createEntryLocked(shard: Shard, handler: Handler, runnable: Runnable): Entry {
        val key = EntryKey(handler, runnable, shard.referenceQueue)
        val entry = Entry(key, mutableListOf())
        shard.entriesByKey[key] = entry
        shard.keysByHandlerHash.getOrPut(key.handlerHash) { mutableSetOf() }.add(key)
        shard.keysByOriginalHash.getOrPut(key.originalHash) { mutableSetOf() }.add(key)
        return entry
    }

    private fun removeEntryLocked(shard: Shard, key: EntryKey) {
        if (shard.entriesByKey.remove(key) == null) return
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

    private fun shardFor(handler: Handler): Shard = shards[shardIndex(System.identityHashCode(handler))]

    private fun shardIndex(hash: Int): Int {
        val mixed = hash xor (hash ushr 16)
        return mixed and (SHARD_COUNT - 1)
    }

    private fun tokenMatches(registeredToken: WeakReference<Any>?, requestedToken: Any?): Boolean {
        return requestedToken == null || registeredToken?.get() === requestedToken
    }

    private class Shard {
        val lock = Any()
        val referenceQueue = ReferenceQueue<Any>()
        val entriesByKey = HashMap<PairLookup, Entry>()
        val keysByHandlerHash = HashMap<Int, MutableSet<EntryKey>>()
        val keysByOriginalHash = HashMap<Int, MutableSet<EntryKey>>()
    }

    private interface PairLookup {
        val handlerHash: Int
        val originalHash: Int
        fun handler(): Handler?
        fun original(): Runnable?
    }

    private class LookupKey(
        private val handler: Handler,
        private val original: Runnable,
    ) : PairLookup {
        override val handlerHash: Int = System.identityHashCode(handler)
        override val originalHash: Int = System.identityHashCode(original)

        override fun handler(): Handler = handler

        override fun original(): Runnable = original

        override fun equals(other: Any?): Boolean {
            if (this === other) return true
            if (other !is PairLookup) return false
            return handlerHash == other.handlerHash &&
                originalHash == other.originalHash &&
                handler === other.handler() &&
                original === other.original()
        }

        override fun hashCode(): Int = 31 * handlerHash + originalHash
    }

    private class EntryKey(
        handler: Handler,
        original: Runnable,
        referenceQueue: ReferenceQueue<Any>,
    ) : PairLookup {
        override val handlerHash: Int = System.identityHashCode(handler)
        override val originalHash: Int = System.identityHashCode(original)
        private val handlerRef = EntryReference(handler, referenceQueue, this)
        private val originalRef = EntryReference(original, referenceQueue, this)

        override fun handler(): Handler? = handlerRef.get()

        override fun original(): Runnable? = originalRef.get()

        fun isCleared(): Boolean = handler() == null || original() == null

        override fun equals(other: Any?): Boolean {
            if (this === other) return true
            if (other !is PairLookup) return false
            return handlerHash == other.handlerHash &&
                originalHash == other.originalHash &&
                handler() === other.handler() &&
                original() === other.original()
        }

        override fun hashCode(): Int = 31 * handlerHash + originalHash
    }

    private class EntryReference<T : Any>(
        referent: T,
        queue: ReferenceQueue<Any>,
        val key: EntryKey,
    ) : WeakReference<T>(referent, queue)

    private data class Entry(
        val key: EntryKey,
        val wrappers: MutableList<WrapperEntry>,
    )

    private data class WrapperEntry(
        val wrapper: WeakReference<Runnable>,
        val token: WeakReference<Any>?,
    )

    private companion object {
        const val SHARD_COUNT = 16
    }
}
