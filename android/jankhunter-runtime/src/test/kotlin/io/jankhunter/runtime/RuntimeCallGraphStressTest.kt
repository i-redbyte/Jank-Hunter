package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.lang.management.ManagementFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCallGraphStressTest {
    @Test
    fun millionEventProducerMatrixMakesProgressWithSlowConsumer() {
        val matrix = listOf(1 to 50_000, 2 to 50_000, 8 to 25_000, 16 to 25_000, 32 to 20_000)
        SEEDS.forEach { seed ->
            matrix.forEach { (producerCount, eventsPerProducer) ->
                runScenario(seed, producerCount, eventsPerProducer)
            }
        }
    }

    private fun runScenario(seed: Long, producerCount: Int, eventsPerProducer: Int) {
        val directory = Files.createTempDirectory("jankhunter-graph-stress").toFile()
        val writer = AsyncLogWriter.open(
            directory,
            JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000).build(),
            "main",
        )
        val clock = AtomicLong()
        val graph = RuntimeCallGraph(
            nowMs = clock::incrementAndGet,
            captureScreen = { "screen" },
            captureFlow = { "flow" },
            captureStep = { "step" },
            maxKeys = { 4_096 },
            consumerDelayNanos = 50_000L,
        )
        graph.resetFlushState(writer)
        val start = CountDownLatch(1)
        val done = CountDownLatch(producerCount)
        val jankHunterWaitObserved = AtomicBoolean(false)
        val waitStack = AtomicReference<String?>()
        val threadMxBean = ManagementFactory.getThreadMXBean()
        val producers = List(producerCount) { producer ->
            Thread({
                start.await()
                repeat(eventsPerProducer) { event ->
                    graph.recordEdge(
                        producer.toLong() xor seed,
                        (event.toLong() * 31L + seed) and 63L,
                    )
                }
                done.countDown()
            }, "JankHunterStressProducer-$producer")
        }
        val sampler = Thread({
            while (done.count > 0L) {
                producers.forEach { thread ->
                    val info = threadMxBean.getThreadInfo(thread.id, 32) ?: return@forEach
                    if (info.threadState == Thread.State.BLOCKED || info.threadState == Thread.State.WAITING) {
                        if (info.stackTrace.any {
                                it.className == RuntimeCallGraph::class.java.name ||
                                    it.className.startsWith("${RuntimeCallGraph::class.java.name}\$")
                            }
                        ) {
                            jankHunterWaitObserved.set(true)
                            waitStack.compareAndSet(null, info.stackTrace.joinToString("\n"))
                        }
                    }
                }
                Thread.yield()
            }
        }, "JankHunterStressSampler")
        try {
            producers.forEach(Thread::start)
            sampler.start()
            start.countDown()
            assertTrue(
                "producer progress timed out for $producerCount threads, seed=$seed",
                done.await(20, TimeUnit.SECONDS),
            )
            producers.forEach { it.join(2_000L) }
            sampler.join(2_000L)
            assertTrue(graph.flushBlocking(10_000L))
            val expected = producerCount.toLong() * eventsPerProducer
            assertEquals(expected, graph.attemptedForTest())
            assertEquals(expected, graph.fullyAccountedEventsForTest())
            assertFalse(
                "producer waited inside Jank Hunter for $producerCount threads, seed=$seed:\n${waitStack.get()}",
                jankHunterWaitObserved.get(),
            )
        } finally {
            graph.flushForShutdown()
            graph.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    private companion object {
        val SEEDS = listOf(0x32524L, 0xC0FFEEL, 0x5EEDL)
    }
}
