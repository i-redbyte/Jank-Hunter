package io.jankhunter.runtime.internal.system

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max
import kotlin.math.min

class ObjectRetentionWatcher(
    retainedDelayMs: Long,
    private val forceGcBeforeReport: Boolean = false,
    private val clock: () -> Long = { SystemClock.elapsedRealtime() },
    private val requestGc: () -> Unit = {
        Runtime.getRuntime().gc()
        System.runFinalization()
    },
    private val reporter: (String?, String?, Long, Long) -> Unit = { className, ownerHint, ageMs, count ->
        JankHunter.recordRetained(className, ownerHint, ageMs, count)
    },
) {
    private val delayMs = max(1_000L, retainedDelayMs)
    private val checkIntervalMs = max(500L, min(delayMs / 2L, 2_000L))
    private val running = AtomicBoolean(false)
    private val queue = ReferenceQueue<Any>()
    private val watched = CopyOnWriteArrayList<WatchedReference>()
    private var thread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        thread = Thread({ loop() }, "JankHunterObjectWatcher").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running.set(false)
        thread?.interrupt()
        watched.clear()
    }

    fun watch(instance: Any?, description: String?, ownerHint: String?) {
        if (instance == null || !running.get()) return
        addWatched(instance, description, ownerHint)
    }

    internal fun watchForTest(instance: Any, description: String?, ownerHint: String? = null) {
        addWatched(instance, description, ownerHint)
    }

    private fun addWatched(instance: Any, description: String?, ownerHint: String?) {
        watched += WatchedReference(
            instance,
            queue,
            safeClassName(instance, description),
            ownerHint?.takeIf { it.isNotBlank() },
            clock(),
        )
    }

    private fun loop() {
        while (running.get()) {
            drainCleared()
            checkRetained()
            try {
                Thread.sleep(checkIntervalMs)
            } catch (_: InterruptedException) {
                if (!running.get()) return
            }
        }
    }

    private fun drainCleared() {
        while (true) {
            val ref = queue.poll() as? WatchedReference ?: return
            watched.remove(ref)
        }
    }

    internal fun checkRetained() {
        val now = clock()
        val groups = linkedMapOf<String, RetainedGroup>()
        var shouldRequestGc = false

        for (ref in watched) {
            if (ref.get() == null) {
                watched.remove(ref)
                continue
            }

            val ageMs = now - ref.watchStartedMs
            if (ageMs < delayMs) continue

            if (ref.firstRetainedAtMs == 0L) {
                ref.firstRetainedAtMs = now
                if (forceGcBeforeReport && !ref.gcRequested) {
                    ref.gcRequested = true
                    shouldRequestGc = true
                }
                continue
            }

            val key = ref.className + "\u0000" + ref.ownerHint.orEmpty()
            groups.getOrPut(key) { RetainedGroup(ref.className, ref.ownerHint) }.add(ageMs)
            watched.remove(ref)
        }

        if (shouldRequestGc) {
            requestGc()
        }
        for (group in groups.values) {
            reporter(group.className, group.ownerHint, group.maxAgeMs, group.count)
        }
    }

    private fun safeClassName(instance: Any, description: String?): String {
        return description?.takeIf { it.isNotBlank() } ?: instance.javaClass.name
    }

    private class RetainedGroup(
        val className: String,
        val ownerHint: String?,
    ) {
        var count = 0L
            private set
        var maxAgeMs = 0L
            private set

        fun add(ageMs: Long) {
            count++
            if (ageMs > maxAgeMs) {
                maxAgeMs = ageMs
            }
        }
    }

    private class WatchedReference(
        referent: Any,
        queue: ReferenceQueue<Any>,
        val className: String,
        val ownerHint: String?,
        val watchStartedMs: Long,
    ) : WeakReference<Any>(referent, queue) {
        var firstRetainedAtMs = 0L
        var gcRequested = false
    }
}
