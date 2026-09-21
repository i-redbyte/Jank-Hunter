package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCompletionClearTest {
    @Test fun hookClearReleasesProducerStateWhileWriterCompletionIsStillRunning() = clearDuringCompletion(false)
    @Test fun graphClearReleasesProducerStateWhileWriterCompletionIsStillRunning() = clearDuringCompletion(true)

    private fun clearDuringCompletion(graphMode: Boolean) {
        val directory = Files.createTempDirectory("jankhunter-completion-clear").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val hooks = RuntimeHookEventTransport({ 16 }, { 16 })
        val graph = RuntimeCallGraph({ 1L }, { null }, { 0L }, { 16 })
        val completion = { entered.countDown(); check(release.await(5L, TimeUnit.SECONDS)) }
        var consumer: Thread? = null
        try {
            if (graphMode) {
                graph.resetFlushState(writer)
                graph.recordSemantic(1L, "parent", 2L, "child", 1L, true)
                assertTrue(graph.flushBlocking(2_000L))
                consumer = graph.consumerForTest()
                graph.whenWriterDrained(writer, completion)
                graph.flushForShutdown(1L)
            } else {
                hooks.start(writer)
                assertTrue(hooks.recordMethod(1L, "method"))
                assertTrue(hooks.flushBlocking(2_000L))
                consumer = hooks.consumerForTest()
                hooks.whenWriterDrained(writer, completion)
                hooks.stopAndFlush(1L)
            }
            assertTrue(entered.await(2L, TimeUnit.SECONDS))
            if (graphMode) {
                graph.clear()
                assertEquals(0, graph.registeredProducerCountForTest())
            } else {
                hooks.clear()
                assertEquals(0, hooks.registeredProducerCountForTest())
            }
        } finally {
            release.countDown()
            consumer?.join(2_000L)
            hooks.clear()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }
}
