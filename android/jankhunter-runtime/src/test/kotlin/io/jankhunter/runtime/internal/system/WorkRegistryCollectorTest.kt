package io.jankhunter.runtime.internal.system

import io.jankhunter.runtime.JankHunterWorkRegistryUnavailable
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.RuntimeWorkCleanup
import io.jankhunter.runtime.JankHunterWorkRegistrySource
import java.util.concurrent.Executor
import org.junit.Assert.assertEquals
import org.junit.Test

class WorkRegistryCollectorTest {
    @Test
    fun rejectedCleanupCannotRunInlineOrRestorePublicationAfterStop() {
        lateinit var callback: (JankHunterWorkRegistryResult) -> Unit
        var closed = 0
        var reports = 0
        val source = JankHunterWorkRegistrySource { _, accept ->
            callback = accept
            AutoCloseable { closed++ }
        }
        val collector = WorkRegistryCollector(source, Executor { it.run() }, { 0L }, RuntimeWorkCleanup { false }) { _, _, _ -> reports++ }
        collector.activate()
        collector.sampleOnce()
        collector.stop()
        callback(JankHunterWorkRegistryResult.Snapshot(emptyList(), 0))
        assertEquals(0, reports)
        assertEquals(0, closed)
    }

    @Test
    fun oneQueryInFlightAndLateCompletionCannotCrossStopOrRestart() {
        val callbacks = ArrayList<(JankHunterWorkRegistryResult) -> Unit>()
        var cancelled = 0
        val source = JankHunterWorkRegistrySource { _, accept ->
            callbacks.add(accept)
            AutoCloseable { cancelled++ }
        }
        val reports = ArrayList<JankHunterWorkRegistryResult>()
        val collector = WorkRegistryCollector(source, Executor { it.run() }, { 100L }, RuntimeWorkCleanup { it(); true }) { result, _, _ -> reports.add(result) }
        collector.activate()
        collector.sampleOnce()
        collector.sampleOnce()
        assertEquals(1, callbacks.size)
        collector.stop()
        collector.activate()
        callbacks[0](JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_FAILED))
        assertEquals(0, reports.size)
        collector.sampleOnce()
        callbacks[1](JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_FAILED))
        assertEquals(1, reports.size)
        assertEquals(2, cancelled)
    }

    @Test
    fun stopBeforeRequestReturnsStillClosesItsHandle() {
        var closed = 0
        lateinit var collector: WorkRegistryCollector
        val source = JankHunterWorkRegistrySource { _, _ ->
            collector.stop()
            AutoCloseable { closed++ }
        }
        collector = WorkRegistryCollector(source, Executor { it.run() }, { 0L }, RuntimeWorkCleanup { it(); true }) { _, _, _ -> error("late report") }
        collector.activate()
        collector.sampleOnce()
        collector.stop()
        assertEquals(1, closed)
    }

    @Test
    fun timeoutCancelsQueryAndReportsUnknownInsteadOfAnEmptyRegistry() {
        var now = 0L
        var closed = 0
        val reports = ArrayList<JankHunterWorkRegistryResult>()
        val source = JankHunterWorkRegistrySource { _, _ -> AutoCloseable { closed++ } }
        val collector = WorkRegistryCollector(source, Executor { it.run() }, { now }, RuntimeWorkCleanup { it(); true }, timeoutMs = 100) { result, _, _ -> reports.add(result) }
        collector.activate()
        collector.sampleOnce()
        now = 100
        collector.sampleOnce()
        assertEquals(1, closed)
        assertEquals(listOf(JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_TIMEOUT)), reports)
    }
    @Test
    fun aFailingCancellationCannotSuppressAnAlreadyCompletedObservation() {
        lateinit var callback: (JankHunterWorkRegistryResult) -> Unit
        var reports = 0
        val source = JankHunterWorkRegistrySource { _, accept ->
            callback = accept
            AutoCloseable { throw IllegalStateException("adapter cleanup") }
        }
        val collector = WorkRegistryCollector(source, Executor { it.run() }, { 0L }, RuntimeWorkCleanup { it(); true }) { _, _, _ -> reports++ }
        collector.activate()
        collector.sampleOnce()
        callback(JankHunterWorkRegistryResult.Snapshot(emptyList(), 0))
        assertEquals(1, reports)
        collector.stop()
    }

}
