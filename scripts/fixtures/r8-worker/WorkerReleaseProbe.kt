package com.example.jhsmoke

import android.util.Log
import android.os.Debug
import android.os.SystemClock
import androidx.work.ListenableWorker
import io.jankhunter.runtime.JankHunterWorkerOutcome
import io.jankhunter.runtime.JankHunterWorkerRuntime

object WorkerReleaseProbe {
    @Volatile private var benchmarkSink = 0
    private class Success
    private class Failure
    private class Retry

    @JvmStatic
    fun run() {
        val cases = listOf(
            ListenableWorker.Result.success() to JankHunterWorkerOutcome.SUCCESS,
            ListenableWorker.Result.failure() to JankHunterWorkerOutcome.FAILURE,
            ListenableWorker.Result.retry() to JankHunterWorkerOutcome.RETRY,
            Success() to JankHunterWorkerOutcome.UNKNOWN,
            Failure() to JankHunterWorkerOutcome.UNKNOWN,
            Retry() to JankHunterWorkerOutcome.UNKNOWN,
            null to JankHunterWorkerOutcome.UNKNOWN,
        )
        benchmark(cases.take(3).map { it.first }.toTypedArray())
        repeat(2) {
            cases.forEachIndexed { index, (value, expected) ->
                val hook = LegacyWorkerCaller.classify(value)
                val port = JankHunterWorkerRuntime.classify(value)
                val expectedCode = when (expected) {
                    JankHunterWorkerOutcome.SUCCESS -> 0
                    JankHunterWorkerOutcome.FAILURE -> 1
                    JankHunterWorkerOutcome.RETRY -> 2
                    JankHunterWorkerOutcome.CANCELLED -> 3
                    JankHunterWorkerOutcome.UNKNOWN -> 4
                }
                check(hook == expectedCode && port == expected) {
                    "case=$index hook=$hook port=$port expected=$expected"
                }
                Log.i("JHWORKERR8", "case=$index hook=$hook port=$port type=${value?.javaClass?.name}")
            }
        }
        Log.i("JHWORKERR8", "PASS cases=${cases.size}")
    }

    private fun benchmark(values: Array<Any?>) {
        repeat(10_000) { benchmarkSink = LegacyWorkerCaller.classify(values[it % values.size]) }
        val samples = LongArray(100)
        val allocatedBefore = Debug.getRuntimeStat("art.gc.bytes-allocated")?.toLongOrNull()
        val cpuBefore = Debug.threadCpuTimeNanos()
        var sum = 0
        samples.indices.forEach { sample ->
            val start = SystemClock.elapsedRealtimeNanos()
            repeat(1_000) { sum += LegacyWorkerCaller.classify(values[it % values.size]) }
            samples[sample] = (SystemClock.elapsedRealtimeNanos() - start) / 1_000
        }
        val cpu = Debug.threadCpuTimeNanos() - cpuBefore
        val allocatedAfter = Debug.getRuntimeStat("art.gc.bytes-allocated")?.toLongOrNull()
        benchmarkSink = sum
        samples.sort()
        val allocated = if (allocatedBefore != null && allocatedAfter != null) allocatedAfter - allocatedBefore else -1
        Log.i("JHWORKERR8", "BENCH batchMeanNsP50=${samples[49]} p95=${samples[94]} p99=${samples[98]} " +
            "threadCpuNs=$cpu processAllocatedBytes=$allocated calls=100000 checksum=$sum")
    }
}
