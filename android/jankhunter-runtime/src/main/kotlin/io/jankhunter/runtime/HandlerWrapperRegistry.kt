package io.jankhunter.runtime

import android.os.Handler
import java.lang.ref.WeakReference

internal class HandlerWrapperRegistry(
    private val droppedCounter: (String) -> Unit,
) {
    private val lock = Any()
    private val entries = mutableListOf<Entry>()

    fun register(
        handler: Handler,
        runnable: Runnable,
        token: Any?,
        wrapper: Runnable,
        maxEntries: Int,
        maxWrappers: Int,
    ): Boolean {
        synchronized(lock) {
            cleanLocked()
            val entry = entries.firstOrNull {
                it.handler.get() === handler && it.original.get() === runnable
            }
            if (entry == null && (maxEntries <= 0 || entries.size >= maxEntries)) {
                droppedCounter("jankhunter.handler_wrapper.dropped_entries.count")
                return false
            }
            val resolvedEntry = entry ?: Entry(
                handler = WeakReference(handler),
                original = WeakReference(runnable),
                wrappers = mutableListOf(),
            ).also { entries.add(it) }
            if (maxWrappers <= 0 || resolvedEntry.wrappers.size >= maxWrappers) {
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
        synchronized(lock) {
            cleanLocked()
            return entries
                .firstOrNull { it.handler.get() === handler && it.original.get() === runnable }
                ?.wrappers
                ?.filter { tokenMatches(it.token, token) }
                ?.mapNotNull { it.wrapper.get() }
                ?: emptyList()
        }
    }

    fun unregister(delegate: Runnable, wrapper: Runnable) {
        synchronized(lock) {
            cleanLocked()
            val iterator = entries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.original.get() === delegate) {
                    entry.wrappers.removeAll { it.wrapper.get() == null || it.wrapper.get() === wrapper }
                }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    fun unregister(handler: Handler, runnable: Runnable, token: Any?) {
        synchronized(lock) {
            cleanLocked()
            val iterator = entries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.handler.get() === handler && entry.original.get() === runnable) {
                    entry.wrappers.removeAll { tokenMatches(it.token, token) }
                }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    fun unregister(handler: Handler, token: Any?) {
        synchronized(lock) {
            cleanLocked()
            val iterator = entries.iterator()
            while (iterator.hasNext()) {
                val entry = iterator.next()
                if (entry.handler.get() === handler) {
                    entry.wrappers.removeAll { tokenMatches(it.token, token) }
                }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    fun clear() {
        synchronized(lock) {
            entries.clear()
        }
    }

    private fun cleanLocked() {
        val iterator = entries.iterator()
        while (iterator.hasNext()) {
            val entry = iterator.next()
            if (entry.handler.get() == null || entry.original.get() == null) {
                iterator.remove()
            } else {
                entry.wrappers.removeAll { it.wrapper.get() == null }
                if (entry.wrappers.isEmpty()) {
                    iterator.remove()
                }
            }
        }
    }

    private fun tokenMatches(registeredToken: WeakReference<Any>?, requestedToken: Any?): Boolean {
        return requestedToken == null || registeredToken?.get() === requestedToken
    }

    private data class Entry(
        val handler: WeakReference<Handler>,
        val original: WeakReference<Runnable>,
        val wrappers: MutableList<WrapperEntry>,
    )

    private data class WrapperEntry(
        val wrapper: WeakReference<Runnable>,
        val token: WeakReference<Any>?,
    )
}
