package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncLogSessionFactoryTest {
    @Test
    fun processScopeFingerprintIsOrderIndependentAndLengthDelimited() {
        val first = AsyncLogSessionFactory.processScopeFingerprint(
            linkedSetOf("com.example:sync", "com.example"),
        )
        val reordered = AsyncLogSessionFactory.processScopeFingerprint(
            linkedSetOf("com.example", "com.example:sync"),
        )
        val ambiguousConcatenation = AsyncLogSessionFactory.processScopeFingerprint(
            linkedSetOf("com.example:s", "ynccom.example"),
        )

        assertArrayEquals(first, reordered)
        assertFalse(first.contentEquals(ambiguousConcatenation))
        assertTrue(AsyncLogSessionFactory.processScopeFingerprint(emptySet()).isEmpty())
    }
}
