package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class StableSymbolRegistryTest {
    @Test
    fun storesPrimitiveIdsAcrossGrowthAndHashCollisions() {
        val registry = StableSymbolRegistry(initialCapacity = 2)

        assertEquals(1L, registry.put(0L, "zero"))
        repeat(100) { index ->
            assertEquals(index.toLong() + 2L, registry.put((index * 16 + 1).toLong(), "symbol-$index"))
        }

        assertEquals("zero", registry.get(0L))
        assertEquals(1L, registry.alias(0L))
        repeat(100) { index ->
            assertEquals("symbol-$index", registry.get((index * 16 + 1).toLong()))
            assertEquals(index.toLong() + 2L, registry.alias((index * 16 + 1).toLong()))
        }
        assertNull(registry.get(2L))
        assertEquals(0L, registry.alias(2L))
    }
}
