package io.jankhunter.runtime.internal.system

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ProcCpuSamplerTest {
    @Test
    fun parseProcessTicksHandlesCommandWithSpaces() {
        val stat = processStat(command = "main thread", userTicks = 40, systemTicks = 2)

        assertEquals(42L, ProcCpuSampler.parseProcessTicks(stat))
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

    private fun processStat(
        command: String = "app",
        userTicks: Long,
        systemTicks: Long,
    ): String {
        return "123 ($command) S 0 0 0 0 0 0 0 0 0 0 $userTicks $systemTicks"
    }
}
