package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriterFactory
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkRegistrationStopTest {
    @Test
    fun stoppingCannotWaitForAnExternalRegistrationCancellationCallback() {
        val root = File.createTempFile("jh-registration-stop", "").apply { check(delete()); check(mkdirs()) }
        val config = JankHunterConfig.builder().workerTracingEnabled(true).autoStartCollectors(false).build()
        val writer = AsyncLogWriterFactory().open(root, config, "test")
        val nextWriter = AsyncLogWriterFactory().open(File(root, "next"), config, "next")
        val graph = RuntimeComponentGraph({ 1L }, { 1_000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        graph.state.writer = writer
        graph.coordinator.markStarted(config)
        val entered = CountDownLatch(1)
        val release = CountDownLatch(1)
        val cancelled = CountDownLatch(64)
        val returned = CountDownLatch(1)
        val leases = List(64) { index ->
            checkNotNull(graph.workerTelemetry.observeRegistration(graph.workerTelemetry.registrationEpoch(), Runnable {
                try {
                    if (index == 0) { entered.countDown(); check(release.await(5, TimeUnit.SECONDS)) }
                } finally { cancelled.countDown() }
            }))
        }
        val stopping = Thread {
            synchronized(graph.state.lifecycleLock) { graph.coordinator.beginStop() }
            returned.countDown()
        }
        try {
            stopping.start()
            assertTrue(entered.await(2, TimeUnit.SECONDS))
            assertTrue("Runtime stop waited for a foreign cancellation callback", returned.await(1, TimeUnit.SECONDS))
            leases.forEach { assertFalse(it.isActive()) }
            graph.state.writer = nextWriter
            graph.coordinator.markStarted(config)
            val fresh = checkNotNull(graph.workerTelemetry.observeRegistration(graph.workerTelemetry.registrationEpoch(), Runnable {}))
            release.countDown()
            assertTrue(cancelled.await(2, TimeUnit.SECONDS))
            assertTrue("Delayed cancellation from the old epoch affected the new registration", fresh.isActive())
            fresh.close()
        } finally {
            release.countDown()
            stopping.join(2_000)
            assertTrue(cancelled.await(2, TimeUnit.SECONDS))
            graph.coordinator.beginStop()
            writer.close(2_000)
            nextWriter.close(2_000)
            root.deleteRecursively()
        }
        assertFalse(stopping.isAlive)
    }
}
