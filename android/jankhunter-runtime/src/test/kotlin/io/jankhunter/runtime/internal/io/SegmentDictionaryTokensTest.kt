package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertTrue
import org.junit.Test

class SegmentDictionaryTokensTest {
    @Test
    fun reusesPackageClassAndMethodComponentsAcrossNonAdjacentNames() {
        val tokens = SegmentDictionaryTokens()
        val front = DictionaryFrontState(16)
        val encoded = BinaryPayload()
        var tokenBytes = 0
        var taggedFrontBytes = 0
        val values = arrayOf(
            "ru.mail.im.feature.feed.FeedPresenter.render",
            "com.google.android.material.button.MaterialButton.draw",
            "ru.mail.im.feature.feed.FeedPresenter.bind",
            "com.google.android.material.button.MaterialButton.measure",
        )

        for (value in values) {
            val bytes = value.encodeToByteArray()
            val prefix = front.commonPrefix(BinaryLogWriter.DICT_STABLE_SYMBOL, bytes)
            assertTrue(tokens.prepare(BinaryLogWriter.DICT_STABLE_SYMBOL, bytes, prefix, encoded))
            tokenBytes += encoded.size
            taggedFrontBytes += uvarintSize(prefix.toLong() shl 1) +
                uvarintSize((bytes.size - prefix).toLong()) + bytes.size - prefix
            tokens.commit(bytes)
            front.commit(BinaryLogWriter.DICT_STABLE_SYMBOL, bytes)
        }

        assertTrue("token=$tokenBytes front=$taggedFrontBytes", tokenBytes < taggedFrontBytes)
    }

    @Test
    fun ignoresNonCodeDictionaryKinds() {
        val encoded = BinaryPayload().uvarint(99L)
        val prepared = SegmentDictionaryTokens().prepare(
            BinaryLogWriter.DICT_ROUTE,
            "GET /messages".encodeToByteArray(),
            0,
            encoded,
        )

        assertTrue(!prepared)
    }

    private fun uvarintSize(rawValue: Long): Int {
        var value = rawValue
        var size = 1
        while (value and 0x7fL.inv() != 0L) {
            value = value ushr 7
            size++
        }
        return size
    }
}
