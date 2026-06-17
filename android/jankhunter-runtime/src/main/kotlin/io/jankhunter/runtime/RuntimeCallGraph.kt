package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeCallGraph(
    private val nowMs: () -> Long,
    private val captureContext: (ownerOverride: String?) -> JankHunterContext,
    private val maxKeys: () -> Int,
) {
    private val stack = ThreadLocal<MutableList<RuntimeCallFrame>>()
    private val counters = ConcurrentHashMap<RuntimeCallKey, RuntimeCallStats>()
    private val dropped = AtomicLong(0L)
    private val lastFlushAtMs = AtomicLong(0L)

    fun resetFlushState() {
        lastFlushAtMs.set(0L)
        dropped.set(0L)
    }

    fun clear() {
        counters.clear()
        stack.remove()
        dropped.set(0L)
    }

    fun enter(ownerName: String?, enabled: Boolean): Long {
        if (!enabled) return 0L
        val normalizedOwner = normalizedContextValue(ownerName) ?: return 0L
        val now = nowMs()
        val currentStack = stack.get() ?: ArrayList<RuntimeCallFrame>(16).also(stack::set)
        currentStack.add(RuntimeCallFrame(normalizedOwner, now))
        return now
    }

    fun exit(startMs: Long, ownerName: String?, writer: AsyncLogWriter?) {
        if (startMs <= 0L || writer == null) return
        val normalizedOwner = normalizedContextValue(ownerName) ?: return
        val currentStack = stack.get() ?: return
        if (currentStack.isEmpty()) {
            stack.remove()
            return
        }
        val frame = popFrame(currentStack, normalizedOwner, startMs)
        val caller = currentStack.lastOrNull()?.owner
        val durationMs = maxOf(0L, nowMs() - frame.startMs)
        if (caller != null && caller != frame.owner) {
            recordEdge(caller, frame.owner, durationMs, writer)
        }
        if (currentStack.isEmpty()) {
            stack.remove()
        }
    }

    fun flush(force: Boolean, writer: AsyncLogWriter?) {
        val asyncWriter = writer ?: return
        val now = nowMs()
        val last = lastFlushAtMs.get()
        if (!force && now - last < RUNTIME_CALL_FLUSH_MS) return
        if (!lastFlushAtMs.compareAndSet(last, now) && !force) return

        counters.forEach { (key, stats) ->
            val count = stats.count.getAndSet(0)
            if (count <= 0) return@forEach
            val totalMs = stats.totalMs.getAndSet(0)
            val maxMs = stats.maxMs.getAndSet(0)
            asyncWriter.runtimeCall(key.screen, key.caller, key.flow, key.step, key.callee, count, totalMs, maxMs)
        }
        counters.forEach { (key, stats) ->
            if (stats.count.get() == 0L) {
                counters.remove(key, stats)
            }
        }
        val droppedCount = dropped.getAndSet(0)
        if (droppedCount > 0) {
            asyncWriter.counter("jankhunter.runtime_call_graph.dropped.count", droppedCount)
        }
    }

    private fun recordEdge(caller: String, callee: String, durationMs: Long, writer: AsyncLogWriter) {
        val tuple = captureContext(caller)
        val key = RuntimeCallKey(tuple.screen, caller, tuple.flow, tuple.step, callee)
        val stats = counters[key] ?: run {
            val limit = maxKeys()
            if (limit <= 0 || counters.size >= limit) {
                dropped.incrementAndGet()
                flush(force = false, writer)
                return
            }
            counters.computeIfAbsent(key) { RuntimeCallStats() }
        }
        stats.count.incrementAndGet()
        stats.totalMs.addAndGet(durationMs)
        stats.updateMax(durationMs)
        flush(force = false, writer)
    }

    private fun popFrame(
        currentStack: MutableList<RuntimeCallFrame>,
        ownerName: String,
        fallbackStartMs: Long,
    ): RuntimeCallFrame {
        val lastIndex = currentStack.lastIndex
        val last = currentStack[lastIndex]
        if (last.owner == ownerName) {
            currentStack.removeAt(lastIndex)
            return last
        }
        for (index in lastIndex - 1 downTo 0) {
            if (currentStack[index].owner == ownerName) {
                return currentStack.removeAt(index)
            }
        }
        return RuntimeCallFrame(ownerName, fallbackStartMs)
    }

    private data class RuntimeCallFrame(
        val owner: String,
        val startMs: Long,
    )

    private data class RuntimeCallKey(
        val screen: String?,
        val caller: String,
        val flow: String?,
        val step: String?,
        val callee: String,
    )

    private class RuntimeCallStats {
        val count = AtomicLong()
        val totalMs = AtomicLong()
        val maxMs = AtomicLong()

        fun updateMax(value: Long) {
            while (true) {
                val current = maxMs.get()
                if (value <= current) return
                if (maxMs.compareAndSet(current, value)) return
            }
        }
    }

    private companion object {
        const val RUNTIME_CALL_FLUSH_MS = 5000L
    }
}
