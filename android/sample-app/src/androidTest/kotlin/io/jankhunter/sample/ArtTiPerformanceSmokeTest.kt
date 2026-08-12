package io.jankhunter.sample

import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Debug
import android.os.Process
import android.os.SystemClock
import android.view.Choreographer
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import kotlin.concurrent.thread
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ArtTiPerformanceSmokeTest {
    @Test
    fun writesBoundedAggregateForCurrentLane() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val context = instrumentation.targetContext
        repeat(WARMUP_ITERATIONS) { runWorkload() }
        val memoryBefore = memorySnapshot()
        val cpuBeforeMs = Process.getElapsedCpuTime()
        val samples = LongArray(MEASURED_ITERATIONS) {
            val start = SystemClock.elapsedRealtimeNanos()
            runWorkload()
            SystemClock.elapsedRealtimeNanos() - start
        }.sortedArray()
        val cpuDeltaMs = Process.getElapsedCpuTime() - cpuBeforeMs
        val frameIntervals = measureFrameIntervals(instrumentation)
        val memoryAfter = memorySnapshot()
        val mode = currentLane(context)
        val json = """{"schema":2,"lane":"$mode","iterations":$MEASURED_ITERATIONS,"workers":$WORKERS,"operations_per_worker":$OPERATIONS_PER_WORKER,"workload_p50_ns":${percentile(samples, 0.50)},"workload_p95_ns":${percentile(samples, 0.95)},"workload_max_ns":${samples.last()},"process_cpu_delta_ms":$cpuDeltaMs,"pss_before_kib":${memoryBefore.totalPss},"pss_after_kib":${memoryAfter.totalPss},"native_pss_before_kib":${memoryBefore.nativePss},"native_pss_after_kib":${memoryAfter.nativePss},"native_heap_before_bytes":${memoryBefore.nativeHeapBytes},"native_heap_after_bytes":${memoryAfter.nativeHeapBytes},"frame_count":${frameIntervals.size},"frame_p50_ns":${percentile(frameIntervals, 0.50)},"frame_p95_ns":${percentile(frameIntervals, 0.95)},"frame_max_ns":${frameIntervals.last()},"slow_frames":${frameIntervals.count { it > SLOW_FRAME_NS }}}"""
        File(context.filesDir, RESULT_FILE).writeText(json)
        instrumentation.sendStatus(
            0,
            Bundle().apply { putString("stream", "JH_ARTTI_PERF $json\n") },
        )
    }

    private fun measureFrameIntervals(instrumentation: android.app.Instrumentation): LongArray {
        val frameTimes = LongArray(FRAME_COUNT)
        val nextIndex = AtomicInteger()
        val complete = CountDownLatch(1)
        val workload = thread(name = "JH-perf-frame-load", start = false) {
            repeat(FRAME_LOAD_ITERATIONS) { runWorkload() }
        }
        lateinit var callback: Choreographer.FrameCallback
        callback = Choreographer.FrameCallback { frameTimeNanos ->
            val index = nextIndex.getAndIncrement()
            if (index < frameTimes.size) frameTimes[index] = frameTimeNanos
            if (index + 1 < frameTimes.size) {
                Choreographer.getInstance().postFrameCallback(callback)
            } else {
                complete.countDown()
            }
        }
        instrumentation.runOnMainSync {
            Choreographer.getInstance().postFrameCallback(callback)
            workload.start()
        }
        assertTrue("frame workload timed out", complete.await(FRAME_TIMEOUT_SECONDS, TimeUnit.SECONDS))
        workload.join(TimeUnit.SECONDS.toMillis(FRAME_TIMEOUT_SECONDS))
        assertTrue("frame workload thread did not finish", !workload.isAlive)
        return LongArray(frameTimes.size - 1) { index -> frameTimes[index + 1] - frameTimes[index] }
            .sortedArray()
    }

    private fun memorySnapshot(): MemorySnapshot {
        Runtime.getRuntime().gc()
        SystemClock.sleep(MEMORY_SETTLE_MS)
        val info = Debug.MemoryInfo()
        Debug.getMemoryInfo(info)
        return MemorySnapshot(
            totalPss = info.totalPss,
            nativePss = info.nativePss,
            nativeHeapBytes = Debug.getNativeHeapAllocatedSize(),
        )
    }

    private fun runWorkload() {
        val checksum = AtomicLong()
        val monitor = Any()
        val workers = List(WORKERS) { worker ->
            thread(name = "JH-perf-$worker") {
                var local = worker.toLong() + 1L
                repeat(OPERATIONS_PER_WORKER) { iteration ->
                    local = (local * 1_103_515_245L + iteration + 12_345L) xor (local ushr 7)
                    if (iteration and 255 == 0) synchronized(monitor) { checksum.addAndGet(local and 0xff) }
                }
                checksum.addAndGet(local)
            }
        }
        workers.forEach(Thread::join)
        assertEquals(WORKERS, workers.size)
        check(checksum.get() != 0L)
    }

    @Suppress("DEPRECATION")
    private fun currentLane(context: android.content.Context): String {
        val metadata = context.packageManager
            .getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
            .metaData
        if (metadata?.getBoolean("io.jankhunter.enabled", false) != true) return "baseline"
        val options = metadata.getString("io.jankhunter.artti.native_options").orEmpty()
        return when {
            "profile=2" in options -> "causal"
            options.isEmpty() -> "off"
            else -> "profile_unknown"
        }
    }

    private fun percentile(sorted: LongArray, quantile: Double): Long {
        val index = ((sorted.size - 1) * quantile).toInt().coerceIn(sorted.indices)
        return sorted[index]
    }

    private companion object {
        const val RESULT_FILE = "artti-benchmark-result.json"
        const val WARMUP_ITERATIONS = 8
        const val MEASURED_ITERATIONS = 40
        const val WORKERS = 8
        const val OPERATIONS_PER_WORKER = 20_000
        const val FRAME_COUNT = 64
        const val FRAME_LOAD_ITERATIONS = 8
        const val FRAME_TIMEOUT_SECONDS = 15L
        const val MEMORY_SETTLE_MS = 100L
        const val SLOW_FRAME_NS = 24_000_000L
    }

    private data class MemorySnapshot(
        val totalPss: Int,
        val nativePss: Int,
        val nativeHeapBytes: Long,
    )
}
