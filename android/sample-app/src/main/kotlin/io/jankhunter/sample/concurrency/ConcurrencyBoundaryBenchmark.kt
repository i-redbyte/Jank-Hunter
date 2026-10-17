package io.jankhunter.sample.concurrency

import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Debug
import android.os.Process
import android.os.SystemClock
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterManifestConfig
import io.jankhunter.runtime.JankHunterResources
import io.jankhunter.runtime.JankHunterRuntimeFeature
import io.jankhunter.runtime.JankHunterExecutionObservationMode
import io.jankhunter.sample.benchmarkcontrol.submitUnobserved
import java.io.File
import java.util.concurrent.Callable
import java.util.concurrent.Executors
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import java.util.zip.ZipFile
import org.json.JSONArray
import org.json.JSONObject

/** Repeatable CPU workload, with separate submission-boundary and whole-process measurements. */
internal class ConcurrencyBoundaryBenchmark(
    private val context: Context,
    private val beforeMeasuredPhase: (() -> Unit)? = null,
    private val afterMeasuredPhase: (() -> JSONObject)? = null,
    private val observationMode: JankHunterExecutionObservationMode = JankHunterExecutionObservationMode.WRAPPERS,
) {
    @Volatile private var checksum = 0L
    fun run(secondsPerMode: Int): File {
        require(secondsPerMode in 5..600)
        val fixedInPlace = context.packageManager.getApplicationInfo(context.packageName, PackageManager.GET_META_DATA)
            .metaData?.getBoolean("io.jankhunter.sample.benchmark_in_place_only") == true
        require(!fixedInPlace || observationMode == JankHunterExecutionObservationMode.IN_PLACE) {
            "Fixed IN_PLACE benchmark APK cannot measure WRAPPERS"
        }
        val phases = JSONArray()
        try {
            listOf("disabled", "normal", "detailed").forEach { mode -> phases.put(runPhase(mode, secondsPerMode)) }
        } finally {
            JankHunter.shutdown()
        }
        val artifact = File(context.getExternalFilesDir(null) ?: context.filesDir, "concurrency-benchmark.json")
        artifact.writeText(JSONObject()
            .put("device", Build.FINGERPRINT)
            .put("android_api", Build.VERSION.SDK_INT)
            .put("cores", Runtime.getRuntime().availableProcessors())
            .put("seconds_per_mode", secondsPerMode)
            .put("observation_mode", observationMode.name)
            .put("build_strategy", if (fixedInPlace) "fixed_in_place_benchmark" else "dual")
            .put("task_cpu_target_ns", TASK_CPU_NS)
            .put("phases", phases)
            .put("scope", if (afterMeasuredPhase == null) {
                "isolated serial Executor workload; process CPU includes recording/GC; no UI frame workload"
            } else {
                "serial Executor plus constant animated View; process CPU includes recording/GC/drawing"
            })
            .toString(2))
        return artifact
    }

    private fun runPhase(mode: String, seconds: Int): JSONObject {
        JankHunter.shutdown()
        val directory = File(context.filesDir, "concurrency-benchmark-$mode")
        directory.deleteRecursively()
        check(directory.mkdirs())
        val builder = JankHunterManifestConfig.read(context).toBuilder()
            .taskObservationMode(observationMode)
            .coroutineObservationMode(observationMode)
            .enabled(mode != "disabled")
            .logDirectory(directory)
            .concurrencyDetailedEnabled(mode == "detailed")
            .systemSamplerEnabled(false)
            .memorySampleIntervalMs(600_000)
            .runtimeCallGraphEnabled(false)
        JankHunterRuntimeFeature.entries.forEach { builder.runtimeFeatureEnabled(it, false) }
        if (mode != "disabled") {
            builder.runtimeFeatureEnabled(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS, true)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.EXECUTORS, true)
                .runtimeFeatureEnabled(JankHunterRuntimeFeature.THREAD_SAMPLING, true)
        }
        JankHunter.init(context, builder.build())
        check(JankHunter.executionObservationModes().tasks == observationMode &&
            JankHunter.executionObservationModes().coroutines == observationMode) {
            "Benchmark mode was fixed before explicit init; build with -PjankHunterAutoInit=false"
        }
        val executor = Executors.newSingleThreadExecutor()
        val lock = ReentrantLock()
        val task = Callable {
            JankHunterResources.withLock(lock) {
                val end = Debug.threadCpuTimeNanos() + TASK_CPU_NS
                while (Debug.threadCpuTimeNanos() < end) checksum = checksum * 31L + 1L
            }
        }
        val samples = LongArray(MAX_SAMPLES)
        var count = 0L
        var peakJavaHeap = 0L
        val elapsedStart: Long
        val processCpuStart: Long
        val elapsedNs: Long
        val processCpuMs: Long
        val frames: JSONObject?
        val paired: JSONObject
        val allocatedBefore: Long?
        val allocatedBytes: Long?
        try {
            repeat(WARMUP_TASKS) { executor.submit(task).get(5, TimeUnit.SECONDS) }
            beforeMeasuredPhase?.invoke()
            elapsedStart = SystemClock.elapsedRealtimeNanos()
            processCpuStart = Process.getElapsedCpuTime()
            allocatedBefore = Debug.getRuntimeStat("art.gc.bytes-allocated")?.toLongOrNull()
            peakJavaHeap = usedJavaHeap()
            val deadline = elapsedStart + TimeUnit.SECONDS.toNanos(seconds.toLong())
            while (SystemClock.elapsedRealtimeNanos() < deadline) {
                val boundaryStart = SystemClock.elapsedRealtimeNanos()
                val result = executor.submit(task)
                val boundaryNs = SystemClock.elapsedRealtimeNanos() - boundaryStart
                samples[(count % samples.size).toInt()] = boundaryNs
                count++
                result.get(5, TimeUnit.SECONDS)
                if (count % 128L == 0L) peakJavaHeap = maxOf(peakJavaHeap, usedJavaHeap())
            }
            elapsedNs = SystemClock.elapsedRealtimeNanos() - elapsedStart
            processCpuMs = Process.getElapsedCpuTime() - processCpuStart
            val allocatedEnd = Debug.getRuntimeStat("art.gc.bytes-allocated")?.toLongOrNull()
            val before = allocatedBefore
            allocatedBytes = if (before != null && allocatedEnd != null && allocatedEnd >= before) allocatedEnd - before else null
            frames = afterMeasuredPhase?.invoke()
            paired = measurePairedLatency(executor, task)
        } finally {
            executor.shutdownNow()
            check(executor.awaitTermination(5, TimeUnit.SECONDS))
        }
        var logBytes = 0L
        var archiveBytes = 0L
        if (mode != "disabled") {
            val archive = File(context.getExternalFilesDir(null) ?: context.filesDir, "concurrency-benchmark-$mode.zip")
            check(!archive.exists() || archive.delete()) { "cannot replace this benchmark's previous archive" }
            val captured = checkNotNull(JankHunter.captureLogArchive(archive)) {
                "benchmark writer stopped: ${JankHunter.initDiagnostics()}"
            }
            archiveBytes = captured.archiveBytes
            logBytes = ZipFile(archive).use { zip ->
                zip.entries().asSequence().filterNot { it.isDirectory }.sumOf { entry ->
                    check(entry.size >= 0L) { "captured log has no uncompressed size" }
                    entry.size
                }
            }
        }
        JankHunter.shutdown()
        val measured = samples.copyOf(minOf(count, samples.size.toLong()).toInt()).apply { sort() }
        return JSONObject()
            .put("mode", mode).put("tasks", count).put("elapsed_ns", elapsedNs)
            .put("process_cpu_ms", processCpuMs)
            .put("cpu_ms_per_task", processCpuMs.toDouble() / count)
            .put("loop_process_allocated_bytes", allocatedBytes ?: JSONObject.NULL)
            .put("paired_end_to_end_added_latency", paired)
            .put("submit_p50_ns", measured[measured.lastIndex / 2])
            .put("submit_p95_ns", measured[(measured.lastIndex * 95) / 100])
            .put("submission_samples", measured.size)
            .put("submission_sample_policy", "last at most 20000 warmed submission boundaries")
            .put("log_bytes", logBytes)
            .put("archive_bytes", archiveBytes)
            .put("java_heap_used_bytes", usedJavaHeap())
            .put("java_heap_peak_checkpoint_bytes", peakJavaHeap)
            .put("heap_scope", "whole Java process, checkpoint every 128 tasks; not registry retained size")
            .put("frames", frames ?: JSONObject.NULL)
    }

    private fun <T> measurePairedLatency(executor: ExecutorService, task: Callable<T>): JSONObject {
        val differences = LongArray(256)
        for (index in differences.indices) {
            val observedFirst = index % 2 == 0
            val first = timedSubmission(executor, task, observedFirst)
            val second = timedSubmission(executor, task, !observedFirst)
            differences[index] = if (observedFirst) first - second else second - first
        }
        differences.sort()
        return JSONObject().put("samples", differences.size)
            .put("p50_ns", differences[differences.lastIndex / 2])
            .put("p95_ns", differences[differences.lastIndex * 95 / 100])
            .put("min_ns", differences.first()).put("max_ns", differences.last())
            .put("scope", "signed paired end-to-end observed submission minus uninstrumented submission; same executor/task; alternating order; body hooks shared; not total SDK overhead")
    }

    private fun <T> timedSubmission(executor: ExecutorService, task: Callable<T>, observed: Boolean): Long {
        val start = SystemClock.elapsedRealtimeNanos()
        val future = if (observed) executor.submit(task) else submitUnobserved(executor, task)
        future.get(5L, TimeUnit.SECONDS)
        return SystemClock.elapsedRealtimeNanos() - start
    }

    private fun usedJavaHeap(): Long = Runtime.getRuntime().totalMemory() - Runtime.getRuntime().freeMemory()

    private companion object {
        const val TASK_CPU_NS = 1_000_000L
        const val WARMUP_TASKS = 200
        const val MAX_SAMPLES = 20_000
    }
}
