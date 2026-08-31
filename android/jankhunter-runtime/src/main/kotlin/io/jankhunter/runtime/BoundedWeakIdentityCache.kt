package io.jankhunter.runtime

import java.lang.ref.WeakReference

/** Fixed-capacity identity cache whose entries never keep either object alive. */
internal class BoundedWeakIdentityCache<K : Any, V : Any>(capacity: Int) {
    @PublishedApi
    internal val entries: Array<WeakEntry<K, V>?>

    @PublishedApi
    internal var nextReplacement = 0

    init {
        require(capacity > 0) { "cache capacity must be positive" }
        entries = arrayOfNulls(capacity)
    }

    inline fun getOrPut(key: K, create: () -> V): V = synchronized(this) {
        var vacant = -1
        for (index in entries.indices) {
            val entry = entries[index]
            if (entry == null) {
                if (vacant < 0) vacant = index
                continue
            }
            val cachedKey = entry.key.get()
            val cachedValue = entry.value.get()
            if (cachedKey == null || cachedValue == null) {
                entries[index] = null
                if (vacant < 0) vacant = index
            } else if (cachedKey === key) {
                return@synchronized cachedValue
            }
        }

        val value = create()
        val target = if (vacant >= 0) vacant else nextReplacement
        entries[target] = WeakEntry(key, value)
        nextReplacement = if (target + 1 == entries.size) 0 else target + 1
        value
    }

    @PublishedApi
    internal class WeakEntry<K : Any, V : Any>(key: K, value: V) {
        val key = WeakReference(key)
        val value = WeakReference(value)
    }
}
