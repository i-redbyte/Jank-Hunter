package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BitLruCacheTest {
    @Test
    fun evictsLeastRecentlyUsedKey() {
        val cache = BitLruCache<String>(capacity = 2)

        assertNull(cache.admit("first").evicted)
        assertNull(cache.admit("second").evicted)
        assertTrue(cache.touch("first"))

        assertEquals("second", cache.admit("third").evicted)

        assertTrue(cache.contains("first"))
        assertTrue(cache.contains("third"))
        assertFalse(cache.contains("second"))
    }

    @Test
    fun reusesSlotAfterRemoval() {
        val cache = BitLruCache<String>(capacity = 2)

        cache.admit("first")
        cache.admit("second")
        assertTrue(cache.remove("first"))

        val admission = cache.admit("third")

        assertTrue(admission.admitted)
        assertNull(admission.evicted)
        assertEquals(2, cache.size)
        assertTrue(cache.contains("second"))
        assertTrue(cache.contains("third"))
    }

    @Test
    fun rejectsWhenCapacityIsZero() {
        val cache = BitLruCache<String>(capacity = 0)

        val admission = cache.admit("first")

        assertFalse(admission.admitted)
        assertNull(admission.evicted)
        assertEquals(0, cache.size)
    }
}
