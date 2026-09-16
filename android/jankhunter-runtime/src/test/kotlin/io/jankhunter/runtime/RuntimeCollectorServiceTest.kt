package io.jankhunter.runtime

import io.jankhunter.runtime.internal.system.RuntimeMaintenanceScheduler
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeCollectorServiceTest {
    @Test
    fun resetPreservesProcessWideHeapDumpAttribution() {
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.heapDumpInProgress.set(true)
        graph.state.heapDumpAttributionUntilMs.set(Long.MAX_VALUE)

        graph.collectors.reset()

        assertTrue(graph.state.heapDumpInProgress.get())
        assertEquals(Long.MAX_VALUE, graph.state.heapDumpAttributionUntilMs.get())
    }

    @Test
    fun stoppingProducersKeepsMaintenanceAvailableForFinalFlush() {
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        val scheduler = RuntimeMaintenanceScheduler()
        graph.state.maintenanceScheduler = scheduler

        try {
            graph.collectors.stopProducers()

            assertTrue(scheduler.executeAndWait(1_000L) {})

            graph.collectors.shutdownMaintenance()
            assertFalse(scheduler.execute {})
        } finally {
            scheduler.shutdown()
        }
    }
}
