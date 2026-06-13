package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.io.IOException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

class AsyncLogWriter private constructor(
    file: File,
    config: JankHunterConfig,
) {
    private val queue = ArrayBlockingQueue<Action>(config.maxQueueSize())
    private val running = AtomicBoolean(true)
    private val dropped = AtomicLong()
    private val writer = BinaryLogWriter(file)
    private val worker = Thread({ loop() }, "JankHunterWriter").apply {
        isDaemon = true
        start()
    }

    fun session(appVersion: String?, build: String?, device: String?, sdkInt: Int) {
        offer { it.session(appVersion, build, device, sdkInt) }
    }

    fun screen(screen: String?) {
        offer { it.screen(screen) }
    }

    fun http(
        owner: String?,
        route: String?,
        durationMs: Long,
        dnsMs: Long,
        connectMs: Long,
        ttfbMs: Long,
        statusClass: Int,
        rxBytes: Long,
        txBytes: Long,
        flags: Long,
    ) {
        offer { it.http(owner, route, durationMs, dnsMs, connectMs, ttfbMs, statusClass, rxBytes, txBytes, flags) }
    }

    fun stall(owner: String?, stackHint: String?, durationMs: Long) {
        offer { it.stall(owner, stackHint, durationMs) }
    }

    fun memory(pssKb: Long, javaHeapKb: Long, nativeHeapKb: Long) {
        offer { it.memory(pssKb, javaHeapKb, nativeHeapKb) }
    }

    fun uiWindow(
        screen: String?,
        windowMs: Long,
        frameCount: Long,
        jankCount: Long,
        p50Ms: Long,
        p95Ms: Long,
        p99Ms: Long,
    ) {
        offer { it.uiWindow(screen, windowMs, frameCount, jankCount, p50Ms, p95Ms, p99Ms) }
    }

    fun counter(name: String?, value: Long) {
        offer { it.counter(name, value) }
    }

    fun gauge(name: String?, value: Long) {
        offer { it.gauge(name, value) }
    }

    fun close() {
        running.set(false)
        worker.interrupt()
        try {
            worker.join(1000)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
        try {
            writer.close()
        } catch (_: IOException) {
        }
    }

    private fun offer(action: Action) {
        if (!queue.offer(action)) {
            dropped.incrementAndGet()
        }
    }

    private fun loop() {
        while (running.get() || queue.isNotEmpty()) {
            try {
                queue.poll(250, TimeUnit.MILLISECONDS)?.write(writer)
                val lost = dropped.getAndSet(0)
                if (lost > 0) {
                    writer.counter("jankhunter.events_dropped.count", lost)
                }
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            } catch (_: IOException) {
            }
        }
    }

    fun interface Action {
        @Throws(IOException::class)
        fun write(writer: BinaryLogWriter)
    }

    companion object {
        fun open(directory: File, config: JankHunterConfig): AsyncLogWriter {
            if (!directory.exists() && !directory.mkdirs()) {
                error("Cannot create Jank Hunter log directory: $directory")
            }
            val file = File(directory, "session-${System.currentTimeMillis()}.jhlog")
            return try {
                AsyncLogWriter(file, config)
            } catch (e: IOException) {
                throw IllegalStateException("Cannot open Jank Hunter log file", e)
            }
        }
    }
}
