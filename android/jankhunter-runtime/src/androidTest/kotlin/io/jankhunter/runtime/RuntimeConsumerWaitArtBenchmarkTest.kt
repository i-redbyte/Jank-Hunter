package io.jankhunter.runtime

import android.os.Debug
import android.os.Process
import android.os.SystemClock
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.AsyncWriterTerminalObserver
import io.jankhunter.runtime.internal.io.LogQualityCounters
import io.jankhunter.runtime.internal.io.QualityCounterId
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicIntegerArray
import java.util.concurrent.atomic.AtomicLongArray
import java.util.concurrent.atomic.AtomicReference
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith

/** Identical opt-in workloads on ART. Thread scheduling counts are not device wake-from-sleep counts. */
@RunWith(AndroidJUnit4::class)
class RuntimeConsumerWaitArtBenchmarkTest {
    @Test
    fun measureConsumerWaits() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val arguments = InstrumentationRegistry.getArguments()
        assumeTrue(arguments.getString("jankhunterConsumerWaitBenchmark") == "true")
        val label = requireNotNull(arguments.getString("jankhunterBenchmarkLabel"))
        require(label.matches(Regex("[a-zA-Z0-9_-]+")))
        val output = File(instrumentation.context.filesDir, "jankhunter-consumer-wait-$label.jsonl")
        output.writeText("")
        val directory = File(instrumentation.targetContext.cacheDir, "art-consumer-wait-$label")
        val consumers = Consumers(directory)
        try {
            consumers.start()
            consumers.flushAll()
            // Begin after the startup/control signals have been consumed, before the 5s hook deadline.
            Thread.sleep(200L)
            val before = Array(3) { consumers.snapshot(it) }
            val processBefore = Process.getElapsedCpuTime()
            val started = System.nanoTime()
            Thread.sleep(2_000L)
            val elapsed = System.nanoTime() - started
            val processCpu = Process.getElapsedCpuTime() - processBefore
            for (index in 0..2) {
                val after = consumers.snapshot(index)
                val switches = after.switches - before[index].switches
                output.appendText(JSONObject().put("label", label).put("phase", "idle")
                    .put("consumer", NAMES[index]).put("wall_ns", elapsed).put("process_cpu_ms", processCpu)
                    .put("thread_cpu_ns", after.cpuNs - before[index].cpuNs).put("voluntary_switches", switches)
                    .put("consumer_iterations", after.loops - before[index].loops)
                    .put("rss_kb", statusValue(File("/proc/self/status"), "VmRSS:")).toString() + "\n")
                if (arguments.getString("jankhunterAssertIdleDeadline") == "true") {
                    assertTrue("${NAMES[index]} repeatedly woke without input: $switches", switches <= 8L)
                }
            }
            for (onMain in booleanArrayOf(true, false)) {
                for (index in 0..2) {
                    val task = Runnable {
                        val firstStarted = System.nanoTime()
                        consumers.record(index)
                        consumers.flush(index)
                        val first = System.nanoTime() - firstStarted
                        val samples = LongArray(EVENTS)
                        repeat(WARMUP) { consumers.record(index) }
                        consumers.flush(index)
                        val allocated = allocationCount()
                        val cpu = Debug.threadCpuTimeNanos()
                        val wall = System.nanoTime()
                        repeat(EVENTS) { event ->
                            val eventStarted = System.nanoTime()
                            consumers.record(index)
                            samples[event] = System.nanoTime() - eventStarted
                        }
                        val wallNs = System.nanoTime() - wall
                        val cpuNs = Debug.threadCpuTimeNanos() - cpu
                        val allocatedBytes = allocationCount() - allocated
                        consumers.flush(index)
                        samples.sort()
                        output.appendText(JSONObject().put("label", label).put("phase", "active")
                            .put("consumer", NAMES[index]).put("main", onMain).put("events", EVENTS)
                            .put("first_record_flush_ns", first).put("wall_ns", wallNs).put("thread_cpu_ns", cpuNs)
                            .put("process_allocated_bytes", allocatedBytes).put("p50_ns", samples[EVENTS / 2])
                            .put("p95_ns", samples[EVENTS * 95 / 100]).put("p99_ns", samples[EVENTS * 99 / 100])
                            .toString() + "\n")
                    }
                    if (onMain) instrumentation.runOnMainSync(task) else task.run()
                }
            }
            consumers.flushAll()
            consumers.assertAccounting()
            output.appendText(JSONObject().put("label", label).put("phase", "accounting")
                .put("writer_records", consumers.writerRecords).put("hook_events", consumers.hooks.acceptedForTest())
                .put("graph_events", consumers.graph.acceptedForTest()).put("lost", 0L).toString() + "\n")
        } catch (error: Throwable) {
            try {
                consumers.flushAll()
            } catch (flushFailure: Throwable) {
                error.addSuppressed(flushFailure)
            }
            output.appendText(consumers.failureSnapshot().put("label", label).put("phase", "failure").put("error", error.stackTraceToString()).toString() + "\n")
            throw error
        } finally {
            consumers.close()
            directory.deleteRecursively()
        }
    }

    private class Consumers(directory: File) {
        private val tids = AtomicIntegerArray(3)
        private val threads = arrayOfNulls<Thread>(3)
        private val loops = AtomicLongArray(3)
        private val started = CountDownLatch(3)
        private val quality = LogQualityCounters()
        private val failure = AtomicReference<String?>()
        var writerRecords = 0L
            private set
        val writer = AsyncLogWriter(
            directory, JankHunterConfig.builder().autoStartCollectors(false).flushIntervalMs(60_000L).build(),
            "wait", setOf("wait"), true, 1L, "2026-09-14", 1L, { 1L }, quality, { null },
            AsyncWriterTerminalObserver { _, reason, error -> failure.set("reason=$reason error=$error") },
            workerThreadFactory = { task, name -> thread(0, task, name) },
        )
        val hooks = RuntimeHookEventTransport(
            { 16 }, { 16 }, consumerLoopObserver = { loops.incrementAndGet(1) },
            consumerThreadFactory = { task, name -> thread(1, task, name) },
        )
        val graph = RuntimeCallGraph(
            nowMs = SystemClock::elapsedRealtime, captureScreen = { null }, captureOperationId = { 0L },
            maxKeys = { 16 }, consumerLoopObserver = { loops.incrementAndGet(2) },
            consumerThreadFactory = { task, name -> thread(2, task, name) },
        )

        fun start() {
            record(0)
            hooks.start(writer)
            graph.resetFlushState(writer)
            assertTrue(started.await(5L, TimeUnit.SECONDS))
        }

        fun record(index: Int) {
            when (index) {
                0 -> {
                    writer.counter("app.consumer.wait", 1L)
                    if (++writerRecords and 255L == 0L) assertTrue(writer.flushBlocking(5_000L))
                }
                1 -> assertTrue(hooks.recordMethod(1L, "app.Consumer.call"))
                2 -> graph.recordSemantic(2L, "app.Parent", 3L, "app.Child", 1L, true)
            }
        }

        fun flush(index: Int) {
            val flushed = when (index) {
                1 -> hooks.flushBlocking(5_000L)
                2 -> graph.flushBlocking(5_000L)
                else -> writer.flushBlocking(5_000L)
            }
            if (!flushed) throw AssertionError("${NAMES[index]} flush failed: ${failure.get()}, quality=${quality.snapshot()}")
        }

        fun flushAll() {
            flush(1)
            flush(2)
            flush(0)
        }

        fun assertAccounting() {
            val expected = (WARMUP + EVENTS + 1L) * 2L
            assertEquals(expected, hooks.acceptedForTest())
            assertEquals(expected, hooks.emittedForTest())
            assertEquals(0L, hooks.acceptedLossForTest())
            assertEquals(expected, graph.acceptedForTest())
            assertEquals(expected, graph.emittedForTest())
            assertEquals(0L, graph.acceptedEventLossForTest())
            val counters = quality.snapshot().associate { it.counterId to it.value }
            assertEquals(counters[QualityCounterId.ACCEPTED_EVENT_TOTAL], counters[QualityCounterId.WRITTEN_EVENT_TOTAL])
            assertEquals(0L, counters[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
        }

        fun failureSnapshot(): JSONObject = JSONObject()
            .put("hook_attempted", hooks.attemptedForTest()).put("hook_accepted", hooks.acceptedForTest())
            .put("hook_emitted", hooks.emittedForTest()).put("hook_accepted_loss", hooks.acceptedLossForTest())
            .put("hook_accepting", hooks.acceptingPublishersForTest())
            .put("hook_consumer_state", hooks.consumerForTest()?.state?.name)
            .put("graph_attempted", graph.attemptedForTest()).put("graph_accepted", graph.acceptedForTest())
            .put("graph_emitted", graph.emittedForTest()).put("writer_failure", failure.get())
            .put("quality", JSONObject(quality.snapshot().associate { it.counterId.toString() to it.value }))
            .put("writer_queue", writerQueueSnapshot())
            .put("consumer_stacks", JSONObject(threads.withIndex().associate { indexed ->
                NAMES[indexed.index] to (indexed.value?.state?.name + "\n" + indexed.value?.stackTrace?.joinToString("\n"))
            }))

        private fun writerQueueSnapshot(): JSONObject {
            fun field(owner: Any, name: String): Any = checkNotNull(owner.javaClass.getDeclaredField(name)
                .apply { isAccessible = true }.get(owner))
            val producer = field(writer, "producer") as io.jankhunter.runtime.internal.io.AsyncWriterProducer
            val signal = field(producer, "workerWake")
            val snapshot = JSONObject().put("has_events", producer.eventLanes.hasEvents())
                .put("permits", producer.queuedEvents.availablePermits())
                .put("wake_pending", (field(signal, "pending") as java.util.concurrent.atomic.AtomicBoolean).get())
            for (name in arrayOf("bulk", "critical")) {
                val lane = field(producer.eventLanes, name)
                val consumed = (field(lane, "consumerPosition") as java.util.concurrent.atomic.AtomicLong).get()
                val segment = field(lane, "consumerSegment")
                val start = field(segment, "startPosition") as Long
                val elements = field(segment, "elements") as java.util.concurrent.atomic.AtomicReferenceArray<*>
                val offset = (consumed - start).toInt()
                val head = if (offset in 0 until elements.length()) elements.get(offset) else null
                snapshot.put(name, JSONObject().put("producer", (field(lane, "producerPosition") as java.util.concurrent.atomic.AtomicLong).get())
                    .put("consumer", consumed).put("segment_start", start).put("head_type", head?.javaClass?.simpleName))
            }
            return snapshot
        }

        fun snapshot(index: Int): ThreadSnapshot {
            val task = File("/proc/self/task/${tids.get(index)}")
            return ThreadSnapshot(
                statusValue(File(task, "status"), "voluntary_ctxt_switches:"),
                File(task, "schedstat").readText().trim().split(' ')[0].toLong(), loops.get(index),
            )
        }

        fun close() {
            assertTrue(hooks.stopAndFlush(5_000L))
            hooks.clear()
            assertTrue(graph.flushForShutdown(5_000L))
            graph.clear()
            assertTrue(writer.close(5_000L))
        }

        private fun thread(index: Int, task: Runnable, name: String) = Thread({
            tids.set(index, Process.myTid())
            started.countDown()
            task.run()
        }, name).also { threads[index] = it }
    }

    private data class ThreadSnapshot(val switches: Long, val cpuNs: Long, val loops: Long)

    private companion object {
        val NAMES = arrayOf("writer", "hooks", "graph")
        const val WARMUP = 500_000
        const val EVENTS = 50_000

        fun allocationCount(): Long = Debug.getRuntimeStat("art.gc.bytes-allocated").toLong()

        fun statusValue(file: File, key: String): Long = file.useLines { lines ->
            lines.first { it.startsWith(key) }.removePrefix(key).trim().substringBefore(' ').toLong()
        }
    }
}
