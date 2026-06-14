package io.jankhunter.runtime.internal.system

import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import kotlin.math.max

class MainThreadWatchdog(
    private val thresholdMs: Long,
) {
    private val mainHandler = Handler(Looper.getMainLooper())
    private val running = AtomicBoolean(false)
    private val lastBeatMs = AtomicLong()
    private var thread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        lastBeatMs.set(SystemClock.elapsedRealtime())
        beat()
        thread = Thread({ loop() }, "JankHunterMainWatchdog").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running.set(false)
        thread?.interrupt()
    }

    private fun beat() {
        mainHandler.post(
            object : Runnable {
                override fun run() {
                    lastBeatMs.set(SystemClock.elapsedRealtime())
                    if (running.get()) {
                        mainHandler.postDelayed(this, max(100L, thresholdMs / 2))
                    }
                }
            },
        )
    }

    private fun loop() {
        var lastReportedAt = 0L
        while (running.get()) {
            val now = SystemClock.elapsedRealtime()
            val delay = now - lastBeatMs.get()
            if (delay >= thresholdMs && now - lastReportedAt >= thresholdMs) {
                lastReportedAt = now
                val stack = Looper.getMainLooper().thread.stackTrace
                val stackHint = stack.firstOrNull()?.let { "${it.className}.${it.methodName}" } ?: "unknown"
                JankHunter.recordStall(JankHunter.currentOwner(), stackHint, delay)
            }
            try {
                Thread.sleep(max(100L, thresholdMs / 2))
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            }
        }
    }
}
