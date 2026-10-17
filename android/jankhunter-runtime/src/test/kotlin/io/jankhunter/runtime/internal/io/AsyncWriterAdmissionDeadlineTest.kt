package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterConfig
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncWriterAdmissionDeadlineTest {
    @Test
    fun diagnosticCounterNeverWaitsForWriterAdmission() = withBlockedAdmission { writer ->
        val done = CountDownLatch(1)
        val publisher = Thread { try { writer.counter("diagnostic", 1L) } finally { done.countDown() } }
        publisher.start()
        try { assertTrue("counter producer waited for admission", done.await(100L, TimeUnit.MILLISECONDS)) }
        finally { publisher.interrupt() }
    }

    @Test
    fun saturatedBulkLanePreservesAReservedCausalStartEndPair() {
        val directory = Files.createTempDirectory("jankhunter-causal-critical-reserve").toFile()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val writer = AsyncLogWriter(
            directory, JankHunterConfig.builder().maxQueueSize(16).build(), "priority", setOf("priority"), true,
            1L, "2026-10-14", 1L, { 1L }, LogQualityCounters(), { null }, AsyncWriterTerminalObserver { _, _, _ -> },
            workerThreadFactory = { task, name -> Thread({ entered.countDown(); release.await(); task.run() }, name) },
        )
        try {
            val bulk = CausalEventObservation(CausalKind.OPERATION, 1, 1L, observedNanos = 1L)
            assertTrue(writer.causalEvent(bulk))
            assertTrue(entered.await(5L, TimeUnit.SECONDS))
            repeat(15) { assertTrue(writer.causalEvent(bulk.copy(id = it.toLong() + 2L))) }
            assertFalse(writer.causalEvent(bulk.copy(id = 100L)))
            assertTrue(writer.causalEvent(CausalEventObservation(
                CausalKind.EXECUTOR_TASK, CausalPhase.ExecutorTask.RUN_STARTED, 1L, observedNanos = 2L)))
            assertTrue(writer.causalEvent(CausalEventObservation(
                CausalKind.EXECUTOR_TASK, CausalPhase.ExecutorTask.RUN_ENDED, 1L, observedNanos = 3L)))
        } finally {
            release.countDown()
            writer.close(5_000L)
            directory.deleteRecursively()
        }
    }

    @Test
    fun blockingFlushIncludesContendedAdmissionInItsDeadline() = withBlockedAdmission { writer ->
        assertFalse(writer.flushBlocking(25L))
    }

    @Test
    fun closeIncludesContendedAdmissionInItsDeadline() = withBlockedAdmission { writer ->
        assertFalse(writer.close(25L))
    }

    @Test
    fun asynchronousFlushPreservesInterruptWhenAdmissionIsContended() = withBlockedAdmission { writer ->
        Thread.currentThread().interrupt()
        try {
            writer.flush()
            assertTrue(Thread.currentThread().isInterrupted)
        } finally {
            Thread.interrupted()
        }
    }

    @Test
    fun closeCannotReportCompletionWhileAnAdmittedPublisherIsStartingTheWorker() {
        val directory = Files.createTempDirectory("jankhunter-lazy-worker-close").toFile()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val publisher = Executors.newSingleThreadExecutor()
        val writer = AsyncLogWriter(
            directory, JankHunterConfig.builder().build(), "lazy", setOf("lazy"), true,
            1L, "2026-09-14", 1L, { 1L }, LogQualityCounters(), { null },
            AsyncWriterTerminalObserver { _, _, _ -> },
            workerThreadFactory = { task, name ->
                object : Thread(task, name) {
                    override fun start() {
                        entered.countDown()
                        check(release.await(2L, TimeUnit.SECONDS))
                        super.start()
                    }
                }
            },
        )
        try {
            val accepted = publisher.submit { writer.counter("accepted.before.close", 1L) }
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            assertFalse("close reported success before its accepted publisher started the writer", writer.close(25L))
            release.countDown()
            accepted.get(1L, TimeUnit.SECONDS)
            assertTrue(writer.close(1_000L))
        } finally {
            release.countDown()
            publisher.shutdown()
            assertTrue(publisher.awaitTermination(2L, TimeUnit.SECONDS))
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }

    @Test
    fun crashCounterRecordingNeverWaitsForWriterAdmission() = withBlockedAdmission { writer ->
        assertFalse(writer.recordCrash())
    }

    @Test
    fun crashDiagnosticsRejectSaturationWithoutSequenceGapsOrUnaccountedEvents() {
        val directory = Files.createTempDirectory("jankhunter-crash-saturation").toFile()
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val quality = LogQualityCounters()
        val writer = AsyncLogWriter(
            directory, JankHunterConfig.builder().build(), "crash", setOf("crash"), true,
            1L, "2026-09-14", 1L, { 1L }, quality, { null }, AsyncWriterTerminalObserver { _, _, _ -> },
            workerThreadFactory = { task, name -> Thread({
                entered.countDown()
                check(release.await(2L, TimeUnit.SECONDS))
                task.run()
            }, name) },
        )
        try {
            assertTrue(writer.recordCrash())
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            var accepted = 1L
            while (accepted < 4_096L && writer.recordCrash()) accepted++
            assertTrue("critical lane was not bounded", accepted < 4_096L)
            val before = quality.snapshot().associate { it.counterId to it.value }
            assertEquals(accepted, before[QualityCounterId.ACCEPTED_EVENT_TOTAL])
            assertEquals(1L, before[QualityCounterId.QUEUE_FULL_TOTAL])
            release.countDown()
            assertTrue(writer.close(1_000L))
            val after = quality.snapshot().associate { it.counterId to it.value }
            assertEquals(accepted, after[QualityCounterId.WRITTEN_EVENT_TOTAL])
        } finally {
            release.countDown()
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }

    private fun withBlockedAdmission(action: (AsyncLogWriter) -> Unit) {
        val directory = Files.createTempDirectory("jankhunter-control-admission").toFile()
        val writer = AsyncLogWriterFactory().open(
            directory, JankHunterConfig.builder().backgroundAdmissionWaitMs(1_000L).build(), "deadline",
        )
        val workers = Executors.newFixedThreadPool(2)
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        // Hold the real admission lock without introducing a test branch into every producer call.
        val field = AsyncLogWriter::class.java.getDeclaredField("producer").apply { isAccessible = true }
        val producer = field.get(writer) as AsyncWriterProducer
        try {
            writer.counter("bootstrap", 1L)
            assertTrue(writer.flushBlocking(1_000L))
            val owner = workers.submit {
                producer.admissionLock.lock()
                try {
                    entered.countDown()
                    assertTrue(release.await(2L, TimeUnit.SECONDS))
                } finally {
                    producer.admissionLock.unlock()
                }
            }
            assertTrue(entered.await(1L, TimeUnit.SECONDS))
            val control = workers.submit { action(writer) }
            try {
                control.get(250L, TimeUnit.MILLISECONDS)
            } finally {
                release.countDown()
                owner.get(1L, TimeUnit.SECONDS)
            }
        } finally {
            release.countDown()
            workers.shutdown()
            assertTrue(workers.awaitTermination(2L, TimeUnit.SECONDS))
            writer.close(1_000L)
            directory.deleteRecursively()
        }
    }
}
