package io.jankhunter.workmanager

import android.os.Debug
import android.os.Process
import androidx.test.platform.app.InstrumentationRegistry
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequest
import androidx.work.Operation
import androidx.work.WorkManager
import io.jankhunter.runtime.JankHunter
import io.jankhunter.runtime.JankHunterConfig
import java.io.File
import java.util.UUID
import java.util.concurrent.TimeUnit
import org.json.JSONObject
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Public adapter plus actual WorkManager DB and writer; process allocation includes background work. */
class WorkRegistrationArtBenchmarkTest {
    @Test
    fun measurePublicEnqueueAndCompleteObservationCost() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterWorkBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "work-registration-$label.jsonl")
        output.writeText("")
        val manager = WorkManager.getInstance(instrumentation.targetContext)
        for (main in booleanArrayOf(true, false)) {
            for (mode in listOf("accepted", "ignored", "inactive")) {
                val directory = File(output.parentFile, "work-benchmark-session")
                JankHunter.shutdown()
                directory.deleteRecursively()
                val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
                    .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
                instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
                if (mode == "inactive") JankHunter.setRuntimeEnabled(false)
                try {
                    measure(manager, main, mode, output, label)
                } finally {
                    instrumentation.runOnMainSync { JankHunter.shutdown() }
                }
            }
        }
    }

    private fun measure(manager: WorkManager, main: Boolean, mode: String, output: File, label: String) {
        val unique = "jh-benchmark-" + UUID.randomUUID()
        val existing = request()
        val requests = List(EVENTS + WARMUP) { request() }
        manager.enqueueUniqueWork(unique, ExistingWorkPolicy.KEEP, existing).result.get(10L, TimeUnit.SECONDS)
        try {
            val warm = ArrayList<Operation>(WARMUP)
            onThread(main) { repeat(WARMUP) { warm += enqueue(manager, unique, mode, requests[it]) } }
            warm.forEach { it.result.get(10L, TimeUnit.SECONDS) }
            drainQueries(manager)
            JankHunter.flush()
            val operations = arrayOfNulls<Operation>(EVENTS)
            val samples = LongArray(EVENTS)
            var threadCPU = 0L
            val bytesBefore = allocated()
            val processBefore = Process.getElapsedCpuTime()
            val wallBefore = System.nanoTime()
            onThread(main) {
                val cpuBefore = Debug.threadCpuTimeNanos()
                repeat(EVENTS) { index ->
                    val started = System.nanoTime()
                    operations[index] = enqueue(manager, unique, mode, requests[index + WARMUP])
                    samples[index] = System.nanoTime() - started
                }
                threadCPU = Debug.threadCpuTimeNanos() - cpuBefore
            }
            operations.forEach { it!!.result.get(10L, TimeUnit.SECONDS) }
            drainQueries(manager)
            JankHunter.flush()
            val wall = System.nanoTime() - wallBefore
            val processCPU = Process.getElapsedCpuTime() - processBefore
            val bytes = allocated() - bytesBefore
            samples.sort()
            val last = manager.getWorkInfoById(requests.last().id).get(10L, TimeUnit.SECONDS)
            if (mode == "accepted") assertNotNull(last) else assertNull(last)
            output.appendText(JSONObject().put("label", label).put("main", main).put("mode", mode)
                .put("events", EVENTS).put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
                .put("p99_ns", samples[EVENTS * 99 / 100]).put("thread_cpu_ns", threadCPU)
                .put("process_cpu_ms", processCPU).put("complete_wall_ns", wall)
                .put("process_allocated_bytes", bytes).toString() + "\n")
        } finally {
            manager.cancelUniqueWork(unique).result.get(10L, TimeUnit.SECONDS)
            if (mode == "accepted") requests.forEach { manager.cancelWorkById(it.id).result.get(10L, TimeUnit.SECONDS) }
            manager.pruneWork().result.get(10L, TimeUnit.SECONDS)
        }
    }

    private fun enqueue(manager: WorkManager, unique: String, mode: String, request: OneTimeWorkRequest): Operation =
        if (mode == "accepted") manager.enqueueWithJankHunter(request)
        else manager.enqueueUniqueWorkWithJankHunter(unique, ExistingWorkPolicy.KEEP, request)

    private fun onThread(main: Boolean, task: () -> Unit) {
        if (main) InstrumentationRegistry.getInstrumentation().runOnMainSync(task) else task()
    }

    private fun request() = OneTimeWorkRequest.Builder(WorkRegistrationArtWorker::class.java)
        .setInitialDelay(1L, TimeUnit.DAYS).build()

    private companion object {
        const val EVENTS = 512
        const val WARMUP = 128
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
