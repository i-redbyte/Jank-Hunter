package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
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
}
