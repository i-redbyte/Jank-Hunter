package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Test

class WorkRegistryConfigTest {
    @Test fun registrySourceAndIntervalSurviveReconfigurationWithAnExplicitLowerBound() {
        val source = JankHunterWorkRegistrySource { _, _ -> AutoCloseable { } }
        val config = JankHunterConfig.builder().workerRegistrySource(source).workerRegistryIntervalMs(1).build().toBuilder().build()
        assertSame(source, config.workerRegistrySource())
        assertEquals(5000L, config.workerRegistryIntervalMs())
        assertNull(JankHunterConfig.builder().build().workerRegistrySource())
    }
}
