package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class StableSymbolRegistryTest {
    @Test
    fun storesPrimitiveIdsAcrossGrowthAndHashCollisions() {
        val registry = StableSymbolRegistry(initialCapacity = 2)

        repeat(100) { index -> registry.put((index * 16 + 1).toLong(), "symbol-$index") }

        repeat(100) { index ->
            assertEquals("symbol-$index", registry.get((index * 16 + 1).toLong()))
        }
        assertNull(registry.get(2L))
    }
}
