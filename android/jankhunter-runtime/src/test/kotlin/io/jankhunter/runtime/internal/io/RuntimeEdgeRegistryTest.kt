package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class RuntimeEdgeRegistryTest {
    @Test
    fun definitionsAndReferencesUseDenseWireIds() {
        val registry = RuntimeEdgeRegistry(maxEntries = 4, initialCapacity = 1)

        assertEquals(3L, registry.resolve(1L, 2L, 3L, 4L))
        assertEquals(2L, registry.resolve(1L, 2L, 3L, 4L))
        assertEquals(5L, registry.resolve(1L, 2L, 3L, 5L))
        assertEquals(4L, registry.resolve(1L, 2L, 3L, 5L))
        assertEquals(2, registry.entryCount())
    }

    @Test
    fun exactKeysSurviveGrowthAndHashCollisions() {
        val registry = RuntimeEdgeRegistry(maxEntries = 2_048, initialCapacity = 1)
        repeat(1_024) { index ->
            assertEquals(
                ((index + 1).toLong() shl 1) or 1L,
                registry.resolve(index.toLong(), 11L, index.toLong() * 17L, 22L),
            )
        }
        repeat(1_024) { index ->
            assertEquals(
                (index + 1).toLong() shl 1,
                registry.resolve(index.toLong(), 11L, index.toLong() * 17L, 22L),
            )
        }
    }

    @Test
    fun fullRegistryFallsBackToExactInlineTuple() {
        val registry = RuntimeEdgeRegistry(maxEntries = 2, initialCapacity = 1)

        assertEquals(3L, registry.resolve(0L, 1L, 0L, 2L))
        assertEquals(5L, registry.resolve(0L, 1L, 0L, 3L))
        assertEquals(0L, registry.resolve(0L, 1L, 0L, 4L))
        assertEquals(2L, registry.resolve(0L, 1L, 0L, 2L))
    }
}
