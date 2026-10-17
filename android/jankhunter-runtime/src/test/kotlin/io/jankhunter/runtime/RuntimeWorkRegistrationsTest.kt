package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeWorkRegistrationsTest {
    private fun retainedState(lease: RuntimeWorkRegistration): Any? {
        val field = RuntimeWorkRegistration::class.java.getDeclaredField("state").apply { isAccessible = true }
        return (field.get(lease) as java.util.concurrent.atomic.AtomicReference<*>).get()
    }

    @Test
    fun rejectedCancellationStillDetachesTheLeaseWithoutAnInlineFallback() {
        var calls = 0
        val registry = RuntimeWorkRegistrations({ 1L }, RuntimeWorkCleanup { false })
        val lease = checkNotNull(registry.reserve(Runnable { calls++ }) { _, _ -> error("late publication") })
        registry.close()
        assertFalse(lease.isActive())
        lease.observed(1, false)
        assertEquals(null, retainedState(lease))
        assertEquals(0, calls)
    }

    @Test
    fun capacityCompletionTimeoutAndCloseReleaseEveryCallbackOnce() {
        var now = 100L
        val losses = mutableListOf<Int>()
        val pool = RuntimeWorkRegistrations({ now }, RuntimeWorkCleanup { it(); true }, capacity = 2, timeoutMs = 10, lost = losses::add)
        var cancelled = 0
        var published = 0
        val first = checkNotNull(pool.reserve(Runnable { cancelled++ }) { _, _ -> published++ })
        val second = checkNotNull(pool.reserve(Runnable { cancelled++ }) { _, _ -> published++ })
        assertEquals(null, pool.reserve(Runnable { error("unowned callback") }) { _, _ -> })
        first.observed(1, false)
        assertEquals(1, published)
        first.close()
        assertFalse(first.isActive())
        val third = checkNotNull(pool.reserve(Runnable { cancelled++ }) { _, _ -> published++ })
        now = 110
        assertFalse(second.isActive())
        pool.expire()
        second.observed(2, false)
        third.observed(3, false)
        assertEquals(1, published)
        assertEquals(2, cancelled)
        assertEquals(listOf(RuntimeWorkRegistrations.CAPACITY, RuntimeWorkRegistrations.TIMEOUT, RuntimeWorkRegistrations.TIMEOUT), losses)
        pool.close()
        assertEquals(2, cancelled)
        assertEquals(null, pool.reserve(Runnable {}) { _, _ -> })
        assertEquals(3, losses.size)
    }

    @Test
    fun restartDuringRequestCaptureCannotAdmitAnOldBatchIntoTheNewEpoch() {
        val directory = java.nio.file.Files.createTempDirectory("jh-registration-capture").toFile()
        val config = JankHunterConfig.builder().workerTracingEnabled(true).build()
        val factory = io.jankhunter.runtime.internal.io.AsyncLogWriterFactory()
        val first = factory.open(java.io.File(directory, "first"), config, "first")
        val second = factory.open(java.io.File(directory, "second"), config, "second")
        val graph = RuntimeComponentGraph({ 1L }, { 1000L })
        graph.state.bindCurrentConfiguration(config)
        graph.state.runtimeEnabled.set(true)
        try {
            graph.state.writer = first
            graph.coordinator.markStarted(config)
            val epoch = graph.workerTelemetry.registrationEpoch()
            assertTrue(epoch != 0L)
            graph.coordinator.beginStop()
            graph.state.writer = second
            graph.coordinator.markStarted(config)
            assertEquals(null, graph.workerTelemetry.observeRegistration(epoch, Runnable {}))
        } finally {
            graph.coordinator.beginStop()
            first.close(1000)
            second.close(1000)
            directory.deleteRecursively()
        }
    }

    @Test
    fun closingRuntimeEpochDiscardsItsRegistrations() {
        val directory = java.nio.file.Files.createTempDirectory("jh-registration-epoch").toFile()
        val config = JankHunterConfig.builder().build()
        val writer = io.jankhunter.runtime.internal.io.AsyncLogWriterFactory().open(directory, config, "test")
        val epochs = RuntimeCollectionEpochs()
        val cancelled = java.util.concurrent.CountDownLatch(1)
        try {
            epochs.open(writer, config)
            val lease = checkNotNull(checkNotNull(epochs.current).registrations.reserve(Runnable { cancelled.countDown() }) { _, _ ->
                error("publication after epoch closed")
            })
            epochs.close(writer)
            assertFalse(lease.isActive())
            lease.observed(1, false)
            assertEquals(null, retainedState(lease))
            assertTrue(cancelled.await(2, java.util.concurrent.TimeUnit.SECONDS))
        } finally {
            epochs.close()
            writer.close(1000)
            directory.deleteRecursively()
        }
    }

    @Test
    fun oldLeaseCannotRetainOrPublishIntoReplacementCollection() {
        val first = RuntimeWorkRegistrations({ 1L }, RuntimeWorkCleanup { it(); true })
        var oldEvents = 0
        var newEvents = 0
        var cancelled = 0
        val old = checkNotNull(first.reserve(Runnable { cancelled++ }) { _, _ -> oldEvents++ })
        first.close()
        val next = RuntimeWorkRegistrations({ 1L }, RuntimeWorkCleanup { it(); true })
        val current = checkNotNull(next.reserve(Runnable {}) { _, _ -> newEvents++ })
        old.observed(1, false)
        assertFalse(old.isActive())
        assertTrue(current.isActive())
        current.observed(2, false)
        assertEquals(0, oldEvents)
        assertEquals(1, newEvents)
        assertEquals(1, cancelled)
        assertEquals(null, retainedState(old))
        current.close()
        assertEquals(null, retainedState(current))
    }
}
