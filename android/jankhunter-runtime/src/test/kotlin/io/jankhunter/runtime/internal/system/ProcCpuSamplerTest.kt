package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test

class ProcCpuSamplerTest {
    @Test
    fun parserKeepsOnlyPrimitivePreviousState() {
        assertFalse(ProcCpuSampler::class.java.declaredClasses.any { it.simpleName == "Snapshot" })
        assertFalse(ProcCpuSampler::class.java.declaredClasses.any { it.simpleName == "SystemTicks" })
    }

    @Test
    fun parseProcessTicksHandlesCommandWithSpaces() {
        val stat = processStat(command = "main thread", userTicks = 40, systemTicks = 2)

        assertEquals(42L, ProcCpuSampler.parseProcessTicks(stat))
    }

    @Test
    fun parseProcessTicksRejectsOverflow() {
        val stat = processStat(userTicks = Long.MAX_VALUE, systemTicks = 1L)

        assertNull(ProcCpuSampler.parseProcessTicks(stat))
    }

    @Test
    fun malformedSystemFieldDoesNotShiftCpuColumns() {
        val processStats = mutableListOf(
            processStat(userTicks = 10, systemTicks = 10),
            processStat(userTicks = 20, systemTicks = 20),
        )
        val systemStats = mutableListOf(
            "cpu 100 0 100 700 100",
            "cpu 130 broken 100 760 110",
        )
        val sampler = ProcCpuSampler(
            readProcessStat = { processStats.removeAt(0) },
            readSystemStat = { systemStats.removeAt(0) },
            coreCount = { 4 },
        )

        assertNull(sampler.sample())
        assertNull(sampler.sample())
    }

    @Test
    fun firstSampleWarmsBaseline() {
        val processStats = mutableListOf(
            processStat(userTicks = 10, systemTicks = 10),
        )
        val systemStats = mutableListOf(
            "cpu 100 0 100 700 100",
        )
        val sampler = ProcCpuSampler(
            readProcessStat = { processStats.removeAt(0) },
            readSystemStat = { systemStats.removeAt(0) },
            coreCount = { 4 },
        )

        assertNull(sampler.sample())
    }

    @Test
    fun sampleComputesProcessAndDeviceCpuPercentages() {
        val processStats = mutableListOf(
            processStat(userTicks = 10, systemTicks = 10),
            processStat(userTicks = 30, systemTicks = 20),
        )
        val systemStats = mutableListOf(
            "cpu 100 0 100 700 100",
            "cpu 130 0 100 760 110",
        )
        val sampler = ProcCpuSampler(
            readProcessStat = { processStats.removeAt(0) },
            readSystemStat = { systemStats.removeAt(0) },
            coreCount = { 4 },
        )

        sampler.sample()
        val sample = sampler.sample()

        assertEquals(3000L, sample?.processDevicePercentX100)
        assertEquals(12000L, sample?.processCorePercentX100)
        assertEquals(3000L, sample?.deviceBusyPercentX100)
        assertEquals(4, sample?.coreCount)
    }

    @Test
    fun guestTicksAreNotCountedTwiceInSystemTotal() {
        val processStats = mutableListOf(
            processStat(userTicks = 0L, systemTicks = 0L),
            processStat(userTicks = 50L, systemTicks = 0L),
        )
        val systemStats = mutableListOf(
            "cpu 100 0 0 700 0 0 0 0 50 0",
            "cpu 200 0 0 700 0 0 0 0 100 0",
        )
        val sampler = ProcCpuSampler(
            readProcessStat = { processStats.removeAt(0) },
            readSystemStat = { systemStats.removeAt(0) },
            coreCount = { 1 },
        )

        assertNull(sampler.sample())
        val sample = sampler.sample()

        assertEquals(5_000L, sample?.processDevicePercentX100)
        assertEquals(10_000L, sample?.deviceBusyPercentX100)
    }

    @Test
    fun percentageCalculationDoesNotOverflowLargeKernelCounters() {
        val processStats = mutableListOf(
            processStat(userTicks = 0L, systemTicks = 0L),
            processStat(userTicks = Long.MAX_VALUE / 2L + 1L, systemTicks = 0L),
        )
        val systemStats = mutableListOf(
            "cpu 0 0 0 0",
            "cpu ${Long.MAX_VALUE} 0 0 0",
        )
        val sampler = ProcCpuSampler(
            readProcessStat = { processStats.removeAt(0) },
            readSystemStat = { systemStats.removeAt(0) },
            coreCount = { 2 },
        )

        assertNull(sampler.sample())
        val sample = sampler.sample()

        assertEquals(5_000L, sample?.processDevicePercentX100)
        assertEquals(10_000L, sample?.processCorePercentX100)
        assertEquals(10_000L, sample?.deviceBusyPercentX100)
    }

    @Test
    fun processPercentagesAreClampedWhenProcSnapshotsAreNotAtomic() {
        val processStats = mutableListOf(
            processStat(userTicks = 0L, systemTicks = 0L),
            processStat(userTicks = 200L, systemTicks = 0L),
        )
        val systemStats = mutableListOf(
            "cpu 0 0 0 0",
            "cpu 100 0 0 0",
        )
        val sampler = ProcCpuSampler(
            readProcessStat = { processStats.removeAt(0) },
            readSystemStat = { systemStats.removeAt(0) },
            coreCount = { 4 },
        )

        assertNull(sampler.sample())
        val sample = sampler.sample()

        assertEquals(10_000L, sample?.processDevicePercentX100)
        assertEquals(40_000L, sample?.processCorePercentX100)
    }

    private fun processStat(
        command: String = "app",
        userTicks: Long,
        systemTicks: Long,
    ): String {
        return "123 ($command) S 0 0 0 0 0 0 0 0 0 0 $userTicks $systemTicks"
    }
}
