package io.jankhunter.runtime.internal.system

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import java.lang.ref.ReferenceQueue
import java.lang.ref.WeakReference
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.math.max

class ObjectRetentionWatcher(
    retainedDelayMs: Long,
) {
    private val delayMs = max(1_000L, retainedDelayMs)
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

    fun watch(instance: Any?, description: String?) {
        if (instance == null || !running.get()) return
        watched += WatchedReference(
            instance,
            queue,
            description?.takeIf { it.isNotEmpty() } ?: instance.javaClass.name,
            SystemClock.elapsedRealtime(),
        )
    }

    private fun loop() {
        while (running.get()) {
            drainCleared()
            checkRetained()
            try {
                Thread.sleep(delayMs)
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

    private fun checkRetained() {
        val now = SystemClock.elapsedRealtime()
        for (ref in watched) {
            if (ref.get() == null) {
                watched.remove(ref)
                continue
            }
            val ageMs = now - ref.watchStartedMs
            if (ageMs >= delayMs) {
                JankHunter.recordRetained(ref.className, ageMs, 1)
                watched.remove(ref)
            }
        }
    }

    private class WatchedReference(
        referent: Any,
        queue: ReferenceQueue<Any>,
        val className: String,
        val watchStartedMs: Long,
    ) : WeakReference<Any>(referent, queue)
}
