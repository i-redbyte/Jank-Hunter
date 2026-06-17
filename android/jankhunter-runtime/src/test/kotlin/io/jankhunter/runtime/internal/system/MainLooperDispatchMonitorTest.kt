package io.jankhunter.runtime.internal.system

import android.util.Printer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class MainLooperDispatchMonitorTest {
    @Test
    fun startInstallsPrinterOnlyOnce() {
        val installed = mutableListOf<Printer?>()
        val monitor = MainLooperDispatchMonitor(
            thresholdMs = 1L,
            getMessageLogging = { installed.lastOrNull() },
            setMessageLogging = { installed += it },
            clockMs = { 0L },
            recordDispatch = { _, _, _ -> },
        )

        monitor.start()
        monitor.start()

        assertEquals(1, installed.size)
        assertNotNull(installed.single())
    }

    @Test
    fun stopRestoresPreviousPrinterAndLeavesWrapperInactive() {
        var installed: Printer? = null
        var now = 1_000L
        val recordedDurations = mutableListOf<Long>()
        val monitor = MainLooperDispatchMonitor(
            thresholdMs = 1L,
            getMessageLogging = { installed },
            setMessageLogging = { installed = it },
            clockMs = { now },
            recordDispatch = { durationMs, _, _ -> recordedDurations += durationMs },
        )

        monitor.start()
        val printer = requireNotNull(installed)
        printer.println(">>>>> Dispatching to Handler (android.os.Handler) {abc} callback: work")
        now += 5L
        printer.println("<<<<< Finished to Handler (android.os.Handler) {abc} callback: work")

        monitor.stop()
        assertEquals(null, installed)
        printer.println(">>>>> Dispatching to Handler (android.os.Handler) {abc} callback: ignored")
        now += 10L
        printer.println("<<<<< Finished to Handler (android.os.Handler) {abc} callback: ignored")

        assertEquals(listOf(5L), recordedDurations)
    }

    @Test
    fun thresholdIsClampedToPositiveDuration() {
        var installed: Printer? = null
        var now = 1_000L
        val recordedThresholds = mutableListOf<Long>()
        val monitor = MainLooperDispatchMonitor(
            thresholdMs = 0L,
            getMessageLogging = { installed },
            setMessageLogging = { installed = it },
            clockMs = { now },
            recordDispatch = { _, thresholdMs, _ -> recordedThresholds += thresholdMs },
        )

        monitor.start()
        val printer = requireNotNull(installed)
        printer.println(">>>>> Dispatching to Handler (android.os.Handler) {abc} callback: work")
        now += 1L
        printer.println("<<<<< Finished to Handler (android.os.Handler) {abc} callback: work")

        assertEquals(listOf(1L), recordedThresholds)
        assertTrue(recordedThresholds.all { it > 0L })
    }

    @Test
    fun startChainsAndRestoresPreviousPrinter() {
        val previousLines = mutableListOf<String>()
        val previous = Printer { line -> previousLines += line }
        var installed: Printer? = previous
        var now = 1_000L
        val recordedDurations = mutableListOf<Long>()
        val monitor = MainLooperDispatchMonitor(
            thresholdMs = 1L,
            getMessageLogging = { installed },
            setMessageLogging = { installed = it },
            clockMs = { now },
            recordDispatch = { durationMs, _, _ -> recordedDurations += durationMs },
        )

        monitor.start()
        val printer = requireNotNull(installed)
        printer.println(">>>>> Dispatching to Handler (android.os.Handler) {abc} callback: work")
        now += 7L
        printer.println("<<<<< Finished to Handler (android.os.Handler) {abc} callback: work")
        monitor.stop()

        assertSame(previous, installed)
        assertEquals(2, previousLines.size)
        assertEquals(listOf(7L), recordedDurations)
    }

    @Test
    fun stopDoesNotReplacePrinterInstalledByAnotherProfiler() {
        var installed: Printer? = null
        val other = Printer { }
        val monitor = MainLooperDispatchMonitor(
            thresholdMs = 1L,
            getMessageLogging = { installed },
            setMessageLogging = { installed = it },
            clockMs = { 0L },
            recordDispatch = { _, _, _ -> },
        )

        monitor.start()
        installed = other
        monitor.stop()

        assertSame(other, installed)
    }

    @Test
    fun stopKeepsLaterProfilerChainWorkingAndDisablesDispatchRecording() {
        val previousLines = mutableListOf<String>()
        val laterProfilerLines = mutableListOf<String>()
        val previous = Printer { line -> previousLines += line }
        var installed: Printer? = previous
        var now = 1_000L
        val recordedDurations = mutableListOf<Long>()
        val monitor = MainLooperDispatchMonitor(
            thresholdMs = 1L,
            getMessageLogging = { installed },
            setMessageLogging = { installed = it },
            clockMs = { now },
            recordDispatch = { durationMs, _, _ -> recordedDurations += durationMs },
        )

        monitor.start()
        val jankHunterPrinter = requireNotNull(installed)
        val laterProfiler = Printer { line ->
            laterProfilerLines += line
            jankHunterPrinter.println(line)
        }
        installed = laterProfiler

        monitor.stop()
        installed?.println(">>>>> Dispatching to Handler (android.os.Handler) {abc} callback: work")
        now += 10L
        installed?.println("<<<<< Finished to Handler (android.os.Handler) {abc} callback: work")

        assertSame(laterProfiler, installed)
        assertEquals(2, laterProfilerLines.size)
        assertEquals(2, previousLines.size)
        assertEquals(emptyList<Long>(), recordedDurations)
    }
}
