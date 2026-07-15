package io.jankhunter.runtime.internal.io

import java.nio.charset.StandardCharsets
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DictionaryIdsTest {
    @Test
    fun reusesExistingDictionaryIds() {
        val ids = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 64)

        val first = ids.idFor(1, "FeedRepository")
        val second = ids.idFor(1, "FeedRepository")

        assertEquals(first.id, second.id)
        assertNotNull(first.definition)
        assertNull(second.definition)
    }

    @Test
    fun overflowsAfterRegularEntryBudget() {
        val ids = DictionaryIds(maxRegularEntries = 1, maxValueBytes = 64)

        val regular = ids.idFor(1, "one")
        val overflow = ids.idFor(1, "two")
        val anotherOverflow = ids.idFor(1, "three")

        assertEquals("one", regular.definition?.value)
        assertEquals(DictionaryIds.OVERFLOW_VALUE, overflow.definition?.value)
        assertEquals(overflow.id, anotherOverflow.id)
        assertNull(anotherOverflow.definition)
    }

    @Test
    fun truncatesLongValuesByUtf8Budget() {
        val ids = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 5)

        val result = ids.idFor(2, "abcdef")

        assertEquals("abcde", result.definition?.value)
    }

    @Test
    fun zeroByteBudgetUsesOverflowReferenceOnEveryUse() {
        val ids = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 0)

        val first = ids.idFor(2, "first")
        val second = ids.idFor(2, "second")

        assertTrue(first.overflowed)
        assertTrue(first.truncated)
        assertTrue(second.overflowed)
        assertTrue(second.truncated)
        assertEquals(first.id, second.id)
        assertNotNull(first.definition)
        assertNull(second.definition)
    }

    @Test
    fun utf8TruncationNeverSplitsSupplementaryCodePoint() {
        val ids = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 4)

        val exact = ids.idFor(2, "🚀x")
        val tooSmall = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 3).idFor(2, "🚀")

        assertEquals("🚀", exact.definition?.value)
        assertFalse(exact.overflowed)
        assertTrue(exact.truncated)
        assertEquals(DictionaryIds.OVERFLOW_VALUE, tooSmall.definition?.value)
        assertTrue(tooSmall.overflowed)
    }

    @Test
    fun malformedSurrogatesMatchTheUtf8EncodersSingleReplacementByte() {
        val high = "\uD800"
        val low = "\uDC00"

        assertEquals(1, high.toByteArray(StandardCharsets.UTF_8).size)
        assertEquals(1, low.toByteArray(StandardCharsets.UTF_8).size)
        assertEquals(
            high,
            DictionaryIds(maxRegularEntries = 4, maxValueBytes = 1)
                .idFor(2, high)
                .definition
                ?.value,
        )
        assertEquals(
            low,
            DictionaryIds(maxRegularEntries = 4, maxValueBytes = 1)
                .idFor(2, low)
                .definition
                ?.value,
        )
    }

    @Test
    fun defaultBudgetPreservesLongRouteAndUtf8Boundary() {
        val route = "/messages/" + "длинный-сегмент/".repeat(31) + "🚀"

        val result = DictionaryIds().idFor(2, route)

        assertEquals(route, result.definition?.value)
        assertFalse(result.truncated)
        assertFalse(result.overflowed)
        assertTrue(route.toByteArray(StandardCharsets.UTF_8).size > 256)
        assertTrue(route.toByteArray(StandardCharsets.UTF_8).size <= DictionaryIds.DEFAULT_MAX_VALUE_BYTES)
    }
}
