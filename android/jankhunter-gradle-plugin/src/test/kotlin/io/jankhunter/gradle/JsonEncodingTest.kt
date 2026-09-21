package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertSame
import org.junit.Test

class JsonEncodingTest {
    @Test
    fun returnsOriginalStringWhenEscapingIsNotRequired() {
        val value = "owner.example/Screen#render"

        assertSame(value, escapeJsonString(value))
    }

    @Test
    fun escapesJsonSyntaxAndEveryControlCharacter() {
        val value = "\\\"\b\u000c\n\r\t\u0000\u001f"

        assertEquals("\\\\\\\"\\b\\f\\n\\r\\t\\u0000\\u001f", escapeJsonString(value))
    }

    @Test
    fun preservesUnicodeIncludingSurrogatePairs() {
        val value = "Экран 支払い 🚀"

        assertSame(value, escapeJsonString(value))
    }
}
