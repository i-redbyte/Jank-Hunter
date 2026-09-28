package io.jankhunter.runtime

import android.os.Debug
import android.util.Log
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import kotlin.math.max
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ScenarioTelemetryArtTest {
    @After
    fun tearDown() {
        JankHunter.shutdown()
    }

    @Test
    fun scenarioAndStagesUseExistingOperationLifecycle() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "scenario-telemetry-wire")
        directory.deleteRecursively()
        instrumentation.runOnMainSync {
            JankHunter.init(
                instrumentation.targetContext,
                JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
                    .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build(),
            )
        }
        val scenario = maximumScenario()
        val root = scenario.start("chat.open")
        assertTrue(root.id > 0L)
        assertEquals(JankHunterOperationKind.USER, root.kind)
        val success = scenario.startStage("load.messages")
        assertEquals(root.id, success.parentId)
        assertEquals(JankHunterOperationKind.STAGE, success.kind)
        assertTrue(success.success())
        val failure = scenario.startStage("render.messages")
        assertEquals(root.id, failure.parentId)
        assertTrue(failure.failure())
        val cancelled = scenario.startStage("prefetch.attachments")
        assertEquals(root.id, cancelled.parentId)
        assertTrue(cancelled.cancel())
        assertTrue(root.success())
        JankHunter.flush()
        instrumentation.runOnMainSync { JankHunter.shutdown() }

        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals("fixture must contain one finalized runtime segment", 1, files.size)
        files.single().copyTo(
            File(instrumentation.context.filesDir, "scenario-telemetry-5.1.0.jhlog"),
            overwrite = true,
        )
    }

    @Test
    fun scenarioHelperHasAllocationFreeInactivePathAndBoundedActiveCost() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val scenario = JankHunterScenario.create("benchmark.scenario", "1")
        repeat(WARMUP) { scenario.start().success() }
        val inactive = measure(INACTIVE_ITERATIONS) { scenario.start().success() }
        assertTrue(
            "inactive helper has steady-state allocation: $inactive",
            inactive.allocatedBytes / INACTIVE_ITERATIONS <= INACTIVE_ALLOCATION_LIMIT,
        )

        val directory = File(instrumentation.context.filesDir, "scenario-telemetry-benchmark")
        directory.deleteRecursively()
        instrumentation.runOnMainSync {
            JankHunter.init(
                instrumentation.targetContext,
                JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
                    .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build(),
            )
        }
        repeat(WARMUP) { JankHunterTelemetry.startOperation("benchmark.direct").success() }
        val direct = measure(ITERATIONS) { JankHunterTelemetry.startOperation("benchmark.direct").success() }
        JankHunter.flush()
        repeat(WARMUP) { scenario.start().success() }
        val helper = measure(ITERATIONS) { scenario.start().success() }
        JankHunter.flush()
		instrumentation.runOnMainSync { JankHunter.shutdown() }

        Log.i(TAG, "inactive=$inactive direct=$direct helper=$helper")
        assertEquals("helper operation loss", ITERATIONS, helper.accepted)
        assertTrue("helper p99 is unbounded: $helper", helper.p99Ns <= ACTIVE_P99_LIMIT_NS)
        assertTrue("helper allocation is unbounded: $helper", helper.allocatedBytes / ITERATIONS <= ACTIVE_ALLOCATION_LIMIT)
        assertTrue("helper CPU is unbounded: direct=$direct helper=$helper", helper.cpuNs <= max(direct.cpuNs * 4, ACTIVE_CPU_FLOOR_NS))
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals("benchmark must contain one finalized runtime segment", 1, files.size)
        files.single().copyTo(
            File(instrumentation.context.filesDir, "scenario-benchmark-5.1.0.jhlog"),
            overwrite = true,
        )
    }

    private fun maximumScenario(): JankHunterScenario = JankHunterScenario.create(
        "chat.open",
        "2",
        JankHunterOperationAttributes.fromEntries(
            "account", "existing",
            "cache", "warm",
            "network", "wifi",
            "payload", "small",
            "source", "push",
            "experiment", "control",
        ),
    )

    private fun measure(iterations: Int, operation: () -> Boolean): Measurement {
        val samples = LongArray(iterations)
        var accepted = 0
        val allocationBefore = allocated()
        val cpuBefore = Debug.threadCpuTimeNanos()
        val pssBefore = Debug.getPss()
        val rssBefore = rssKb()
        repeat(iterations) { index ->
            val started = System.nanoTime()
            if (operation()) accepted++
            samples[index] = System.nanoTime() - started
        }
        val cpuNs = Debug.threadCpuTimeNanos() - cpuBefore
        val allocatedBytes = allocated() - allocationBefore
        samples.sort()
        return Measurement(
            p50Ns = samples[iterations / 2],
            p95Ns = samples[iterations * 95 / 100],
            p99Ns = samples[iterations * 99 / 100],
            cpuNs = cpuNs,
            allocatedBytes = allocatedBytes,
            pssDeltaKb = Debug.getPss() - pssBefore,
            rssDeltaKb = rssKb() - rssBefore,
            accepted = accepted,
        )
    }

    private fun allocated(): Long = requireNotNull(Debug.getRuntimeStat("art.gc.bytes-allocated")).toLong()

    private fun rssKb(): Long {
        val residentPages = File("/proc/self/statm").readText().substringAfter(' ').substringBefore(' ').toLong()
        return residentPages * 4L
    }

    private data class Measurement(
        val p50Ns: Long,
        val p95Ns: Long,
        val p99Ns: Long,
        val cpuNs: Long,
        val allocatedBytes: Long,
        val pssDeltaKb: Long,
        val rssDeltaKb: Long,
        val accepted: Int,
    )

    private companion object {
        const val TAG = "JHScenarioBenchmark"
        const val WARMUP = 512
        const val INACTIVE_ITERATIONS = 100_000
        const val ITERATIONS = 2_000
        const val INACTIVE_ALLOCATION_LIMIT = 1L
        const val ACTIVE_P99_LIMIT_NS = 5_000_000L
        const val ACTIVE_ALLOCATION_LIMIT = 4_096L
        const val ACTIVE_CPU_FLOOR_NS = 50_000_000L
    }
}
