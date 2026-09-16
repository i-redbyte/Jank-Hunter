package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.locks.LockSupport
import org.junit.Assert.*
import org.junit.Test

class RuntimeGraphWakeRetentionTest {
    @Test
    fun aWakeConsumedDuringConsumerWorkStillPreventsItsLongIdleWait() {
        val root = Files.createTempDirectory("jh-graph-pending-wake").toFile()
        val writer = AsyncLogWriterFactory().open(root, JankHunterConfig.builder().autoStartCollectors(false).build(), "main")
        val beforeWait = CountDownLatch(1)
        val allowWait = CountDownLatch(1)
        val consumed = CountDownLatch(1)
        val reads = AtomicInteger()
        val graph = RuntimeCallGraph({ 0L }, { null }, { 0L }, { 1 }, exactAdmission = { false },
            batchObserver = { consumed.countDown() },
            uptimeNanos = {
                if (reads.incrementAndGet() == 3) {
                    beforeWait.countDown()
                    check(allowWait.await(5L, TimeUnit.SECONDS))
                    // An internal rotation wait can consume unpark's single permit while the
                    // coalesced publication remains pending. Reproduce that scheduling exactly.
                    LockSupport.parkNanos(1L)
                }
                0L
            })
        try {
            graph.resetFlushState(writer)
            assertTrue(beforeWait.await(5L, TimeUnit.SECONDS))
            repeat(RUNTIME_GRAPH_PAGE_MAX_KEYS + 1) { index ->
                graph.recordSemantic(1L, "caller", index + 2L, "callee", 0L, true)
            }
            allowWait.countDown()
            assertTrue("pending work slept until periodic flush after its unpark permit was consumed",
                consumed.await(2L, TimeUnit.SECONDS))
        } finally {
            allowWait.countDown()
            graph.flushForShutdown(5_000L)
            graph.clear()
            writer.close()
            root.deleteRecursively()
        }
    }
}
