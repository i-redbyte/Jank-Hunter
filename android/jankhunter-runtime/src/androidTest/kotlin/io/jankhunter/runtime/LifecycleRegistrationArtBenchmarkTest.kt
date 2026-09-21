package io.jankhunter.runtime

import android.os.Debug
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.system.ObjectRetentionWatcher
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Same accessor and bounded-watcher workload for pre-streaming and current ART builds. */
class LifecycleRegistrationArtBenchmarkTest {
    @Test
    fun measureBoundedRegistration() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("jankhunterLifecycleBenchmark") == "true")
        val label = requireNotNull(args.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(InstrumentationRegistry.getInstrumentation().context.filesDir, "lifecycle-$label.jsonl")
        output.writeText("")
        for (targetCount in intArrayOf(8, 1000)) {
            val watcher = ObjectRetentionWatcher(1000, maxWatchedReferences = 2)
            val running = ObjectRetentionWatcher::class.java.getDeclaredField("running").apply { isAccessible = true }
            (running.get(watcher) as AtomicBoolean).set(true)
            val state = RuntimeState().apply { objectRetentionWatcher = watcher }
            val access = RuntimeTelemetryAccess(state, ContextTracker(), RuntimeCoordinator(state) { 0L }, { 0L }, { 100 })
            val metrics = RuntimeMetricsService(8, { 0L }, { null }, { null }, {}, { false }, { _, _ -> false })
            val telemetry = RuntimeRetentionTelemetry(state, access, metrics) { 0L }
            val targets = Array(targetCount) { Any() }
            val accessor = object : JankHunterLifecycleAccessorV1 {
                override fun jankHunterLifecycleKindV1() = 2
                override fun jankHunterVisitLifecycleTargetsV1(sink: JankHunterLifecycleTargetSinkV1, ownerHint: String?) {
                    for (target in targets) sink.accept(target, "owner")
                }
            }
            fun visit() = telemetry.watchLifecycleObject(accessor, 2, "onDestroyView", "owner")
            try {
                repeat(100) { visit() }
                repeat(3) { run ->
                    val times = LongArray(500)
                    val bytesBefore = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()
                    val cpuBefore = Debug.threadCpuTimeNanos()
                    for (index in times.indices) {
                        val start = System.nanoTime()
                        visit()
                        times[index] = System.nanoTime() - start
                    }
                    val cpu = Debug.threadCpuTimeNanos() - cpuBefore
                    val bytes = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong() - bytesBefore
                    times.sort()
                    val watched = ObjectRetentionWatcher::class.java.getDeclaredField("watched").apply { isAccessible = true }
                    val count = (watched.get(watcher) as List<*>).size
                    assertEquals(2, count)
                    val memory = Debug.MemoryInfo()
                    Debug.getMemoryInfo(memory)
                    output.appendText(JSONObject().put("label", label).put("run", run).put("targets", targetCount)
                        .put("callbacks", times.size).put("watched", count).put("cpu_ns", cpu).put("allocated_bytes", bytes)
                        .put("p50_ns", times[250]).put("p95_ns", times[475]).put("p99_ns", times[495])
                        .put("pss_kb", memory.totalPss).toString() + "\n")
                }
            } finally { watcher.stop() }
        }
    }
}
