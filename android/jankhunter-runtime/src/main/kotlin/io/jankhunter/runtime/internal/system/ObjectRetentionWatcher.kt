package io.jankhunter.runtime.internal.system

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterContext
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max
import kotlin.math.min

internal class ObjectRetentionWatcher(
    retainedDelayMs: Long,
    private val forceGcBeforeReport: Boolean = false,
    private val clock: () -> Long = { SystemClock.elapsedRealtime() },
    private val requestGc: () -> Unit = {
        Runtime.getRuntime().gc()
        System.runFinalization()
    },
    private val reporter: (String?, String?, JankHunterContext?, Long, Long) -> Unit = { className, ownerHint, context, ageMs, count ->
        JankHunter.recordWatchedRetained(className, ownerHint, context, ageMs, count)
    },
) {
    private val delayMs = max(1_000L, retainedDelayMs)
    private val checkIntervalMs = max(500L, min(delayMs / 2L, 2_000L))
    private val running = AtomicBoolean(false)
    private val queue = ReferenceQueue<Any>()
    private val watched = ConcurrentLinkedQueue<WatchedReference>()
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

    fun watch(instance: Any?, description: String?, ownerHint: String?, context: JankHunterContext?) {
        if (instance == null || !running.get()) return
        addWatched(instance, description, ownerHint, context)
    }

    internal fun watchForTest(
        instance: Any,
        description: String?,
        ownerHint: String? = null,
        context: JankHunterContext? = null,
    ) {
        addWatched(instance, description, ownerHint, context)
    }

    private fun addWatched(instance: Any, description: String?, ownerHint: String?, context: JankHunterContext?) {
        watched.add(
            WatchedReference(
                instance,
                queue,
                safeClassName(instance, description),
                ownerHint?.takeIf { it.isNotBlank() },
                context,
                clock(),
            ),
        )
    }

    private fun loop() {
        while (running.get()) {
            checkRetained()
            try {
                Thread.sleep(checkIntervalMs)
            } catch (_: InterruptedException) {
                if (!running.get()) return
            }
        }
    }

    private fun drainCleared(): Boolean {
        var removed = false
        while (true) {
            val ref = queue.poll() as? WatchedReference ?: return removed
            ref.removed = true
            removed = true
        }
    }

    internal fun checkRetained() {
        val now = clock()
        val groups = linkedMapOf<String, RetainedGroup>()
        var shouldRequestGc = false
        var shouldCompact = drainCleared()

        for (ref in watched) {
            if (ref.removed) {
                shouldCompact = true
                continue
            }
            if (ref.get() == null) {
                ref.removed = true
                shouldCompact = true
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
            groups.getOrPut(key) { RetainedGroup(ref.className, ref.ownerHint, ref.context) }.add(ageMs)
            ref.removed = true
            shouldCompact = true
        }

        if (shouldCompact) {
            compactWatched()
        }

        if (shouldRequestGc) {
            requestGc()
        }
        for (group in groups.values) {
            reporter(group.className, group.ownerHint, group.context, group.maxAgeMs, group.count)
        }
    }

    private fun compactWatched() {
        val survivors = ArrayList<WatchedReference>()
        while (true) {
            val ref = watched.poll() ?: break
            if (!ref.removed && ref.get() != null) {
                survivors.add(ref)
            }
        }
        survivors.forEach(watched::add)
    }

    internal fun watchedCountForTest(): Int = watched.count { !it.removed }

    private fun safeClassName(instance: Any, description: String?): String {
        return description?.takeIf { it.isNotBlank() } ?: instance.javaClass.name
    }

    private class RetainedGroup(
        val className: String,
        val ownerHint: String?,
        val context: JankHunterContext?,
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
        val context: JankHunterContext?,
        val watchStartedMs: Long,
    ) : WeakReference<Any>(referent, queue) {
        var firstRetainedAtMs = 0L
        var gcRequested = false
        @Volatile
        var removed = false
    }
}
