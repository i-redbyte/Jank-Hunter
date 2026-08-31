package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class JankHunterOperationAttributesTest {
    @Test
    fun storesBoundedPairsWithoutMapAllocation() {
        val attributes = JankHunterOperationAttributes.of("source", "push", "cache", "empty")

        assertEquals(2, attributes.size)
        assertEquals("source", attributes.key(0))
        assertEquals("push", attributes.value(0))
        assertEquals("cache", attributes.key(1))
        assertEquals("empty", attributes.value(1))
    }

    @Test
    fun rejectsDuplicateAndUnboundedDimensions() {
        assertThrows(IllegalArgumentException::class.java) {
            JankHunterOperationAttributes.fromEntries("source", "push", "source", "list")
        }
        assertThrows(IllegalArgumentException::class.java) {
            JankHunterOperationAttributes.fromEntries(
                "k1", "v1", "k2", "v2", "k3", "v3", "k4", "v4", "k5", "v5",
                "k6", "v6", "k7", "v7", "k8", "v8", "k9", "v9",
            )
        }
    }
}
