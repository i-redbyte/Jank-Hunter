package io.jankhunter.runtime.internal.io

import java.io.ByteArrayInputStream
import java.util.zip.GZIPInputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertSame
import org.junit.Test

class ReusableGzipEncoderTest {
    @Test
    fun repeatedFramesRemainStandardGzipStreams() {
        val samples = listOf(
            ByteArray(0),
            "small payload".encodeToByteArray(),
            ByteArray(256 * 1024) { index -> (index * 31).toByte() },
        )

        ReusableGzipEncoder().use { encoder ->
            var sharedBuffer: ByteArray? = null
            samples.forEach { raw ->
                encoder.encode(raw, raw.size)
                val encoded = encoder.buffer
                sharedBuffer?.let { assertSame(it, encoded) }
                sharedBuffer = encoded
                val decoded = GZIPInputStream(ByteArrayInputStream(encoded, 0, encoder.size)).use { it.readBytes() }

                assertArrayEquals(raw, decoded)
            }
        }
    }
}
