package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DatabaseDescriptorRegistryTest {
    @Test
    fun reusesDescriptorsAndFallsBackToInlineAtTheBound() {
        val registry = DatabaseDescriptorRegistry(maxEntries = 2)
        val result = DatabaseDescriptorRegistry.Resolution()

        registry.resolve(11L, 21L, 31L, 1L, 2L, 3L, result)
        assertEquals(1L, result.id)
        assertTrue(result.definition)

        registry.resolve(11L, 21L, 31L, 1L, 2L, 3L, result)
        assertEquals(1L, result.id)
        assertFalse(result.definition)

        registry.resolve(12L, 22L, 32L, 1L, 2L, 3L, result)
        assertEquals(2L, result.id)
        assertTrue(result.definition)

        registry.resolve(13L, 23L, 33L, 1L, 2L, 3L, result)
        assertEquals(0L, result.id)
        assertTrue(result.definition)
    }
}
