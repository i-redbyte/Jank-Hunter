package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class RuntimeCollectorServiceTest {
    @Test
    fun resetClearsHeapDumpAttributionFromPreviousSession() {
        val graph = RuntimeComponentGraph(nowMs = { 1L }, nowUs = { 1_000L })
        graph.state.heapDumpInProgress.set(true)
        graph.state.heapDumpAttributionUntilMs.set(Long.MAX_VALUE)

        graph.collectors.reset()

        assertFalse(graph.state.heapDumpInProgress.get())
        assertEquals(0L, graph.state.heapDumpAttributionUntilMs.get())
    }
}
