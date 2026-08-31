package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeMetricNamesTest {
    @Test
    fun ownerNamePreservesExistingMetricContract() {
        assertEquals("unknown", metricOwner(null))
        assertEquals("unknown", metricOwner(" \t "))
        assertEquals("image_decode_owner", metricOwner("image  decode\towner"))
        assertEquals("Owner.Name-1", metricOwner("Owner.Name-1"))
    }
}
