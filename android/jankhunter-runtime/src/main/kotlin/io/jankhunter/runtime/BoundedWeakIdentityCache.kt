package io.jankhunter.runtime

import java.lang.ref.WeakReference

/** Fixed-capacity identity cache whose entries never keep either object alive. */
internal class BoundedWeakIdentityCache<K : Any, V : Any>(capacity: Int) {
    private val entries: Array<WeakEntry<K, V>?>
    private var nextReplacement = 0

    init {
        require(capacity > 0) { "cache capacity must be positive" }
        entries = arrayOfNulls(capacity)
    }

    // Factories resolve application classes and may acquire a class-loader lock. Never hold the
    // shared cache monitor across them; another thread can hold that lock while entering a hook.
    // Duplicate cold computations are allowed; publication rechecks the identity atomically.
    inline fun getOrPut(key: K, create: () -> V): V =
        getOrPublish(key, null) ?: checkNotNull(getOrPublish(key, create()))

    @PublishedApi
    @Synchronized
    internal fun getOrPublish(key: K, value: V?): V? {
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
                return cachedValue
            }
        }

        if (value == null) return null
        val target = if (vacant >= 0) vacant else nextReplacement
        entries[target] = WeakEntry(key, value)
        nextReplacement = if (target + 1 == entries.size) 0 else target + 1
        return value
    }

    private class WeakEntry<K : Any, V : Any>(key: K, value: V) {
        val key = WeakReference(key)
        val value = WeakReference(value)
    }
}
