package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeStopHookDrainTest {
    @Test
    fun timedOutHookDrainKeepsItsWriterOpenUntilTheConsumerFinishes() {
        val directory = Files.createTempDirectory("jankhunter-stop-hook-drain").toFile()
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(directory, config, "main")
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 }, consumerLoopObserver = {
            entered.countDown()
            check(release.await(5L, TimeUnit.SECONDS))
        })
        val graph = RuntimeComponentGraph(nowMs = { 10L }, nowUs = { 10_000L })
        graph.state.config = config
        graph.state.writer = writer
        val session = RuntimeSessionController(
            graph.state, graph.coordinator, graph.metrics, graph.sampling, hooks, graph.runtimeCallGraph,
            graph.handlerHooks, graph.asyncTelemetry, graph.collectors, AsyncLogWriterFactory(), { 10L },
        )
        try {
            hooks.start(writer)
            assertTrue(entered.await(2L, TimeUnit.SECONDS))
            assertTrue(hooks.recordMethod(71L, "must-survive-stop-timeout"))
            val consumer = checkNotNull(hooks.consumerForTest())
            session.stop(clearInit = true)
            assertTrue("writer closed before upstream hook drain", writer.isAcceptingEvents())
            release.countDown()
            consumer.join(2_000L)
            assertFalse(consumer.isAlive)
            assertFalse("last upstream owner must close the writer", writer.isAcceptingEvents())
        } finally {
            release.countDown()
            hooks.stopAndFlush(2_000L)
            hooks.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }
}
