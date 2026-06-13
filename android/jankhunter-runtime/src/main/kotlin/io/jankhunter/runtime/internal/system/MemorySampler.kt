package io.jankhunter.runtime.internal.system

import android.content.Context
import android.os.Debug
import io.jankhunter.runtime.JankHunter
import java.util.concurrent.atomic.AtomicBoolean

class MemorySampler(
    @Suppress("UNUSED_PARAMETER") context: Context,
    private val intervalMs: Long,
) {
    private val running = AtomicBoolean(false)
    private var thread: Thread? = null

    fun start() {
        if (!running.compareAndSet(false, true)) return
        thread = Thread({ loop() }, "JankHunterMemorySampler").apply {
            isDaemon = true
            start()
        }
    }

    fun stop() {
        running.set(false)
        thread?.interrupt()
    }

    private fun loop() {
        val info = Debug.MemoryInfo()
        while (running.get()) {
            Debug.getMemoryInfo(info)
            val runtime = Runtime.getRuntime()
            val javaHeapKb = (runtime.totalMemory() - runtime.freeMemory()) / 1024L
            val nativeHeapKb = Debug.getNativeHeapAllocatedSize() / 1024L
            JankHunter.recordMemory(info.totalPss.toLong(), javaHeapKb, nativeHeapKb)
            try {
                Thread.sleep(intervalMs)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
            }
        }
    }
}
