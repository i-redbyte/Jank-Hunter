package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertEquals
import org.junit.Test

class DictionaryFrontStateTest {
    @Test
    fun tracksIndependentKindsAndMaximumUtf8Prefix() {
        val state = DictionaryFrontState(2)
        state.commit(0, "com.example.feed.First".encodeToByteArray())
        state.commit(1, "other.value".encodeToByteArray())

        assertEquals(
            "com.example.feed.".length,
            state.commonPrefix(0, "com.example.feed.Second".encodeToByteArray()),
        )
        assertEquals(0, state.commonPrefix(1, "com.example.feed.Second".encodeToByteArray()))
    }

    @Test
    fun neverSplitsAMultibyteCodePoint() {
        val state = DictionaryFrontState(1)
        state.commit(0, "prefix-А".encodeToByteArray())

        assertEquals("prefix-".length, state.commonPrefix(0, "prefix-Б".encodeToByteArray()))
    }
}
