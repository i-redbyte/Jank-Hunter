package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.Jhlog
import java.io.File
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test

class DatabaseTransactionArtBenchmarkTest {
    @Test
    fun measureShallowAndNestedTransactionReuse() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val arguments = InstrumentationRegistry.getArguments()
        assumeTrue(arguments.getString("jankhunterDatabaseDepthBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "database-depth-$label.jsonl")
        output.writeText("")
        for (main in booleanArrayOf(true, false)) {
            val task = Runnable {
                for (depth in intArrayOf(1, 16)) measure(output, label, main, depth)
            }
            if (main) instrumentation.runOnMainSync(task) else task.run()
        }
    }

    private fun measure(output: File, label: String, main: Boolean, depth: Int) {
        var completed = 0L
        var statements = 0L
        val tracker = DatabaseTransactionTracker(
            completionSink = DatabaseTransactionCompletionSink { _, _, _, _, _, _, _, _, count, _, _ ->
                completed++
                statements += count
            },
            nanoTime = System::nanoTime,
        )
        val database = Any()
        val warmup = if (depth == 1) SHALLOW_WARMUP else WARMUP
        repeat(warmup) { cycle(tracker, database, depth) }
        val samples = LongArray(EVENTS)
        val bytesBefore = allocated()
        val cpuBefore = Debug.threadCpuTimeNanos()
        val started = System.nanoTime()
        repeat(EVENTS) { index ->
            val before = System.nanoTime()
            cycle(tracker, database, depth)
            samples[index] = System.nanoTime() - before
        }
        val wall = System.nanoTime() - started
        val cpu = Debug.threadCpuTimeNanos() - cpuBefore
        val bytes = allocated() - bytesBefore
        assertEquals((warmup + EVENTS).toLong() * depth, completed)
        assertEquals(completed, statements)
        assertEquals(0L, tracker.currentTransactionId())
        samples.sort()
        output.appendText(JSONObject().put("label", label).put("main", main).put("depth", depth)
            .put("cycles", EVENTS).put("transactions", EVENTS * depth).put("wall_ns", wall)
            .put("thread_cpu_ns", cpu).put("process_allocated_bytes", bytes)
            .put("p50_ns", samples[EVENTS / 2]).put("p95_ns", samples[EVENTS * 95 / 100])
            .put("p99_ns", samples[EVENTS * 99 / 100]).toString() + "\n")
    }

    private fun cycle(tracker: DatabaseTransactionTracker, database: Any, depth: Int) {
        repeat(depth) {
            check(tracker.begin(database, 1L, "depth", Jhlog.DATABASE_TRANSACTION_DEFERRED) > 0L)
            tracker.recordStatement(Jhlog.DATABASE_OPERATION_INSERT)
        }
        repeat(depth) {
            check(tracker.markSuccessful(database))
            check(tracker.finish(database, DatabaseFailureKind.OTHER, false))
        }
    }

    private companion object {
        const val SHALLOW_WARMUP = 1_000_000
        const val WARMUP = 100_000
        const val EVENTS = 50_000
        fun allocated(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
    }
}
