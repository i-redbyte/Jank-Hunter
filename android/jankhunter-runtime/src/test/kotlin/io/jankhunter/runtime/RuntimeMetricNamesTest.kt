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

    @Test
    fun websocketOwnerKeyMatchesE2EContract() {
        assertEquals(
            "io_jankhunter_sample_graph_checkoutapi_openwebsocket",
            websocketMetricOwnerKey("io.jankhunter.sample.graph.CheckoutApi"),
        )
    }
}
