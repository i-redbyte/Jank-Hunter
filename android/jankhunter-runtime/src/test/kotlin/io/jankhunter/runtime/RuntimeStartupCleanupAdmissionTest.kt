package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.nio.file.Files
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeStartupCleanupAdmissionTest {
    @Test fun graphHookNeverRunsStartupCompletionCallbacks() = checkCleanup(true)
    @Test fun eventHookNeverRunsStartupCompletionCallbacks() = checkCleanup(false)

    private fun checkCleanup(graph: Boolean) {
        val directory = Files.createTempDirectory("jh-startup-producer-cleanup").toFile()
        val writer = AsyncLogWriterFactory().open(directory, JankHunterConfig.builder().autoStartCollectors(false).build(), "fixture")
        val admitted = CountDownLatch(1)
        val releaseAdmission = CountDownLatch(1)
        val completionEntered = CountDownLatch(1)
        val releaseCompletion = CountDownLatch(1)
        val producerDone = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val observer = { admitted.countDown(); check(releaseAdmission.await(5L, TimeUnit.SECONDS)); Unit }
        val stopped = { completionEntered.countDown(); check(releaseCompletion.await(5L, TimeUnit.SECONDS)); Unit }
        lateinit var produce: () -> Unit
        val producer = Thread {
            try { produce() } catch (error: Throwable) { failure.set(error) }
            finally { producerDone.countDown() }
        }
        val factory: (Runnable, String) -> Thread = { task, name ->
            object : Thread(task, name) {
                override fun start() {
                    producer.start()
                    check(admitted.await(5L, TimeUnit.SECONDS))
                    throw IllegalStateException()
                }
            }
        }
        val session = if (graph) RuntimeCallGraphSession({ 1L }, { null }, { 0L }, { 16 },
            generation = 1L, onStopped = stopped, publisherAdmissionObserver = observer,
            consumerThreadFactory = factory) else null
        val events = if (!graph) RuntimeHookEventSession({ 16 }, { 16 }, onStopped = stopped,
            publisherAdmissionObserver = observer, consumerThreadFactory = factory) else null
        produce = { if (session != null) session.recordSemantic(1L, "root", 2L, "callee", 1L, true)
                    else events!!.recordMethod(1L, "fixture"); Unit }
        try {
            try { if (session != null) session.resetFlushState(writer) {} else events!!.start(writer) {} }
            catch (_: IllegalStateException) { }
            releaseAdmission.countDown()
            assertTrue("startup failure cleanup blocked an application hook", producerDone.await(100L, TimeUnit.MILLISECONDS))
            assertTrue("cleanup did not retain a background owner", completionEntered.await(5L, TimeUnit.SECONDS))
            failure.get()?.let { throw AssertionError(it) }
        } finally {
            releaseAdmission.countDown(); releaseCompletion.countDown(); producer.join(5_000L)
            session?.clear(); events?.clear(); writer.close(); directory.deleteRecursively()
        }
    }
}
