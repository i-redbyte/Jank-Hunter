package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

internal class RuntimeLogSpamService(
    private val nowMs: () -> Long,
    private val config: () -> JankHunterConfig?,
    private val writer: () -> AsyncLogWriter?,
    private val runtimeActive: () -> Boolean,
    private val foreground: () -> Boolean,
    private val captureContext: (String?) -> JankHunterContext,
    private val recordDropCounter: () -> Unit,
) {
    private val counters = ConcurrentHashMap<LogSpamKey, AtomicLong>()
    private val keyAdmissionLock = Any()
    private val lastFlushAtMs = AtomicLong(0L)

    fun record(ownerName: String?, source: String?, level: Int) {
        if (!runtimeActive()) return
        val tuple = captureContext(ownerName)
        val key = LogSpamKey(tuple.screen, tuple.owner, tuple.flow, tuple.step, normalizedContextValue(source), level)
        val counter = counters[key] ?: admitCounter(key) ?: run {
            recordDropCounter()
            flush(force = false)
            return
        }
        counter.incrementAndGet()
        flush(force = false)
    }

    fun flush(force: Boolean) {
        val asyncWriter = writer() ?: return
        val now = nowMs()
        val last = lastFlushAtMs.get()
        if (!force && now - last < LOG_SPAM_FLUSH_MS) return
        if (!lastFlushAtMs.compareAndSet(last, now) && !force) return

        counters.forEach { (key, counter) ->
            val count = counter.getAndSet(0)
            if (count <= 0) return@forEach
            asyncWriter.logSpam(key.screen, key.owner, key.flow, key.step, key.source, key.level, count)
            if (count >= LOG_SPAM_PROBLEM_COUNT) {
                asyncWriter.problemWindow(
                    key.screen,
                    key.owner,
                    key.flow,
                    key.step,
                    "log_spam",
                    LOG_SPAM_FLUSH_MS,
                    count,
                    count,
                    foreground = foreground(),
                )
            }
        }
        counters.forEach { (key, counter) ->
            if (counter.get() == 0L) {
                counters.remove(key, counter)
            }
        }
    }

    fun reset() {
        counters.clear()
        lastFlushAtMs.set(0L)
    }

    private fun admitCounter(key: LogSpamKey): AtomicLong? {
        return synchronized(keyAdmissionLock) {
            counters[key] ?: run {
                val maxKeys = config()?.maxLogSpamKeys() ?: DEFAULT_MAX_LOG_SPAM_KEYS
                if (maxKeys <= 0 || counters.size >= maxKeys) {
                    null
                } else {
                    AtomicLong().also { counters[key] = it }
                }
            }
        }
    }

    private data class LogSpamKey(
        val screen: String?,
        val owner: String?,
        val flow: String?,
        val step: String?,
        val source: String?,
        val level: Int,
    )

    private companion object {
        private const val LOG_SPAM_FLUSH_MS = 5000L
        private const val LOG_SPAM_PROBLEM_COUNT = 50L
        private const val DEFAULT_MAX_LOG_SPAM_KEYS = 2048
    }
}
