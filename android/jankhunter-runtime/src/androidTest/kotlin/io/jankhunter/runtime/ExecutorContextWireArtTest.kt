package io.jankhunter.runtime

import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ExecutorContextWireArtTest {
    @Test
    fun submitterContextReachesWriterAndWorkerContextIsRestored() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val directory = File(instrumentation.context.filesDir, "executor-context-wire")
        JankHunter.shutdown()
        directory.deleteRecursively()
        val config = JankHunterConfig.builder().logDirectory(directory).autoStartCollectors(false)
            .runtimeCallGraphEnabled(false).metricAggregationEnabled(false).build()
        val worker = Executors.newSingleThreadExecutor()
        try {
            instrumentation.runOnMainSync { JankHunter.init(instrumentation.targetContext, config) }
            val queue = ArrayDeque<Runnable>()
            val executor = requireNotNull(JankHunterTelemetry.wrapExecutor(Executor { queue.addLast(it) }, "wire", null))
            var parentId = 0L
            JankHunterTelemetry.setScreen("SubmitScreen")
            JankHunterTelemetry.withOwner("SubmitOwner", Runnable {
                val parent = JankHunterTelemetry.startOperation("submit")
                parentId = parent.id
                executor.execute {
                    JankHunterTelemetry.counter("probe.task", 1L)
                    val child = JankHunterTelemetry.startOperation("execute", JankHunterOperationKind.BACKGROUND)
                    try {
                        assertEquals(parentId, child.parentId)
                        JankHunterTelemetry.counter("probe.child", 1L)
                    } finally {
                        child.close()
                    }
                }
                parent.close()
            })
            assertTrue(parentId > 0L)
            val command = queue.removeFirst()
            JankHunterTelemetry.setScreen("WorkerScreen")
            worker.submit {
                JankHunterTelemetry.withOwner("WorkerOwner", Runnable {
                    command.run()
                    JankHunterTelemetry.counter("probe.worker", 1L)
                })
            }.get(5L, TimeUnit.SECONDS)
            JankHunter.flush()
        } finally {
            worker.shutdownNow()
            instrumentation.runOnMainSync { JankHunter.shutdown() }
        }
        val files = directory.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
        assertEquals("fixture must contain one finalized runtime segment", 1, files.size)
        files.single().copyTo(File(instrumentation.context.filesDir, "executor-context-5.1.0.jhlog"), overwrite = true)
    }
}
