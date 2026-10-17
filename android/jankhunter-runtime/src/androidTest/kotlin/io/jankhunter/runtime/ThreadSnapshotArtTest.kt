package io.jankhunter.runtime

import android.os.Process
import android.os.SystemClock
import android.os.Debug
import android.system.Os
import android.system.OsConstants
import android.util.Log
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.system.ProcThreadReader
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ThreadSnapshotArtTest {
    @Test fun readsRealKernelThreadsAndExportsBoundedSnapshots() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.cacheDir, "thread-snapshot-wire")
        directory.deleteRecursively()
        val graph = RuntimeComponentGraph(SystemClock::elapsedRealtime, { SystemClock.elapsedRealtimeNanos() / 1000L })
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        val reader = ProcThreadReader()
        val timings = LongArray(50)
        repeat(timings.size) { reader.read() }
        Runtime.getRuntime().gc()
        val baseline = measureCost { repeat(timings.size) { SystemClock.elapsedRealtimeNanos() } }
        val before = measureCounters()
        val cpuStart = Debug.threadCpuTimeNanos()
        var count = 0
        var scanAllocatedBytes = 0L
        var scanGcCount = 0L
        try {
            instrumentation.runOnMainSync { graph.lifecycle.init(instrumentation.targetContext, config) }
            val writer = checkNotNull(graph.writer)
            repeat(timings.size) { index ->
                val allocatedBeforeScan = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                val gcBeforeScan = Debug.getRuntimeStat("art.gc.gc-count").toLong()
                val start = SystemClock.elapsedRealtimeNanos()
                val snapshot = reader.read()
                timings[index] = SystemClock.elapsedRealtimeNanos() - start
                scanAllocatedBytes += Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - allocatedBeforeScan
                scanGcCount += Debug.getRuntimeStat("art.gc.gc-count").toLong() - gcBeforeScan
                assertTrue(snapshot.threads.any { it.tid == Process.myTid().toLong() })
                assertTrue(snapshot.threads.any { it.tid == Process.myPid().toLong() })
                count = snapshot.threads.size
                if (index < 3) {
                    writer.threadSnapshot(snapshot, start / 1_000_000, timings[index] / 1_000_000, 5000,
                        Os.sysconf(OsConstants._SC_CLK_TCK))
                    assertTrue(writer.flushBlocking(5000))
                }
            }
        } finally {
            instrumentation.runOnMainSync { graph.lifecycle.shutdown() }
        }
        val cpu = Debug.threadCpuTimeNanos() - cpuStart
        val after = measureCounters()
        Runtime.getRuntime().gc()
        val retained = measureCounters()
        timings.sort()
        val logs = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals(1, logs.size)
        logs.single().copyTo(File(instrumentation.context.filesDir, "thread-snapshot-5.1.0.jhlog"), overwrite = true)
        val result =
            "threads=$count scans=50 p50_ns=${timings[25]} p95_ns=${timings[47]} " +
                "cpu_total_ns=$cpu baseline_cpu_ns=${baseline.cpuNanos} " +
                "gc_delta=${after.gcCount - before.gcCount} " +
                "scan_gc_delta=$scanGcCount scan_allocated_bytes=$scanAllocatedBytes " +
                "allocated_delta_bytes=${after.allocatedBytes - before.allocatedBytes} " +
                "heap_before_bytes=${before.heapBytes} heap_after_bytes=${after.heapBytes} " +
                "heap_after_gc_bytes=${retained.heapBytes} " +
                "native_heap_before_bytes=${before.nativeHeapBytes} native_heap_after_bytes=${after.nativeHeapBytes} " +
                "log_bytes=${logs.single().length()}"
        File(instrumentation.context.filesDir, "thread-snapshot-benchmark.txt").writeText("$result\n")
        Log.i("JankHunterBenchmark", result)
    }

    private fun measureCost(block: () -> Unit): Counters {
        val cpu = Debug.threadCpuTimeNanos()
        block()
        return measureCounters().copy(cpuNanos = Debug.threadCpuTimeNanos() - cpu)
    }

    private fun measureCounters(): Counters {
        val runtime = Runtime.getRuntime()
        return Counters(
            cpuNanos = 0L,
            gcCount = Debug.getRuntimeStat("art.gc.gc-count").toLong(),
            allocatedBytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong(),
            heapBytes = runtime.totalMemory() - runtime.freeMemory(),
            nativeHeapBytes = Debug.getNativeHeapAllocatedSize(),
        )
    }

    private data class Counters(
        val cpuNanos: Long,
        val gcCount: Long,
        val allocatedBytes: Long,
        val heapBytes: Long,
        val nativeHeapBytes: Long,
    )
}
