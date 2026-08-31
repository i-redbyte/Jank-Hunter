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

        val first = ids.lookup(1, "FeedRepository")
        val second = ids.lookup(1, "FeedRepository")

        assertEquals(first.id, second.id)
        assertNotNull(first.definition)
        assertNull(second.definition)
    }

    @Test
    fun overflowsAfterRegularEntryBudget() {
        val ids = DictionaryIds(maxRegularEntries = 1, maxValueBytes = 64)

        val regular = ids.lookup(1, "one")
        val overflow = ids.lookup(1, "two")
        val anotherOverflow = ids.lookup(1, "three")

        assertEquals("one", regular.definition?.value)
        assertEquals(DictionaryIds.OVERFLOW_VALUE, overflow.definition?.value)
        assertEquals(overflow.id, anotherOverflow.id)
        assertNull(anotherOverflow.definition)
    }

    @Test
    fun truncatesLongValuesByUtf8Budget() {
        val ids = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 5)

        val result = ids.lookup(2, "abcdef")

        assertEquals("abcde", result.definition?.value)
    }

    @Test
    fun zeroByteBudgetUsesOverflowReferenceOnEveryUse() {
        val ids = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 0)

        val first = ids.lookup(2, "first")
        val second = ids.lookup(2, "second")

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

        val exact = ids.lookup(2, "🚀x")
        val tooSmall = DictionaryIds(maxRegularEntries = 4, maxValueBytes = 3).lookup(2, "🚀")

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
                .lookup(2, high)
                .definition
                ?.value,
        )
        assertEquals(
            low,
            DictionaryIds(maxRegularEntries = 4, maxValueBytes = 1)
                .lookup(2, low)
                .definition
                ?.value,
        )
    }

    @Test
    fun defaultBudgetPreservesLongRouteAndUtf8Boundary() {
        val route = "/messages/" + "длинный-сегмент/".repeat(31) + "🚀"

        val result = DictionaryIds().lookup(2, route)

        assertEquals(route, result.definition?.value)
        assertFalse(result.truncated)
        assertFalse(result.overflowed)
        assertTrue(route.toByteArray(StandardCharsets.UTF_8).size > 256)
        assertTrue(route.toByteArray(StandardCharsets.UTF_8).size <= DictionaryIds.DEFAULT_MAX_VALUE_BYTES)
    }

    private fun DictionaryIds.lookup(kind: Int, value: String?): LookupSnapshot {
        val result = DictionaryLookupResult()
        resolve(kind, value, result)
        return LookupSnapshot(result.id, result.definition, result.overflowed, result.truncated)
    }

    private data class LookupSnapshot(
        val id: Long,
        val definition: DictionaryIds.Definition?,
        val overflowed: Boolean,
        val truncated: Boolean,
    )
}
