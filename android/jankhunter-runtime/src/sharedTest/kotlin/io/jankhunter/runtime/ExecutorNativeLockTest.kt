package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExecutorNativeLockTest {
    @Test
    fun telemetryCannotTurnImmediateNativeRejectionIntoAWaitForThePoolLock() {
        val locked = CountDownLatch(1)
        val release = CountDownLatch(1)
        val returned = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val pool = object : ThreadPoolExecutor(0, 1, 1, TimeUnit.SECONDS, LinkedBlockingQueue()) {
            override fun terminated() {
                locked.countDown()
                check(release.await(5, TimeUnit.SECONDS))
            }
        }
        // AbortPolicy formats pool.toString(), which itself takes mainLock. Use a legitimate
        // host policy that immediately throws, so the uninstrumented control has no such wait.
        pool.rejectedExecutionHandler = java.util.concurrent.RejectedExecutionHandler { _, _ ->
            throw RejectedExecutionException("rejected")
        }
        val shutdown = Thread {
            try { pool.shutdown() } catch (error: Throwable) { failure.set(error) }
        }
        val root = File.createTempFile("jh-pool-lock", "").apply { check(delete()); check(mkdirs()) }
        val config = JankHunterConfig.builder().autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(root, config, "test")
        val graph = RuntimeComponentGraph({ 1L }, { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        val wrapped = checkNotNull(graph.asyncTelemetry.wrapExecutorService(pool, "native", null))
        val submit = Thread {
            try {
                wrapped.execute { error("Rejected task ran") }
                failure.set(AssertionError("The original rejection was lost"))
            } catch (_: RejectedExecutionException) {
                // The native executor rejects immediately, even while terminated owns mainLock.
            } catch (error: Throwable) { failure.set(error) }
            finally { returned.countDown() }
        }
        try {
            shutdown.start()
            assertTrue(locked.await(2, TimeUnit.SECONDS))
            var nativeRejected = false
            try { pool.execute {} } catch (_: RejectedExecutionException) { nativeRejected = true }
            assertTrue("Uninstrumented control must reject without waiting for mainLock", nativeRejected)
            submit.start()
            assertTrue("JH waited on ThreadPoolExecutor.mainLock before invoking execute", returned.await(1, TimeUnit.SECONDS))
        } finally {
            release.countDown()
            shutdown.join(2_000)
            submit.join(2_000)
            graph.coordinator.beginStop()
            writer.close(2_000)
            root.deleteRecursively()
        }
        assertFalse(shutdown.isAlive)
        assertFalse(submit.isAlive)
        failure.get()?.let { throw AssertionError("submission failed", it) }
    }
}
