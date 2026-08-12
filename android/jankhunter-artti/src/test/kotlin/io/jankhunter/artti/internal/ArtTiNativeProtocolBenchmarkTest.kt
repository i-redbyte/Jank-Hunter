package io.jankhunter.artti.internal

import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.Locale
import kotlin.system.measureNanoTime
import org.junit.Assume.assumeTrue
import org.junit.Test

class ArtTiNativeProtocolBenchmarkTest {
    @Test
    fun decodeBatch256() {
        assumeTrue(System.getProperty("jankhunter.benchmark") == "true")
        val input = batch(RECORD_COUNT)
        val decoder = ArtTiNativeProtocolDecoder()
        var checksum = 0L
        val visitor = ArtTiNativeRecordVisitor { record -> checksum = checksum xor record.producerSequence }
        repeat(WARMUP_SAMPLES) {
            repeat(ITERATIONS_PER_SAMPLE) { decoder.decode(input, input.limit(), visitor) }
        }
        val samples = LongArray(SAMPLES) {
            measureNanoTime {
                repeat(ITERATIONS_PER_SAMPLE) {
                    check(decoder.decode(input, input.limit(), visitor).status == ArtTiNativeStatus.OK)
                }
            }
        }.sortedArray()
        benchmarkSink = checksum
        val medianNs = samples[samples.size / 2].toDouble() / ITERATIONS_PER_SAMPLE
        val perRecordNs = medianNs / RECORD_COUNT
        println(
            "JankHunter ART TI benchmark: batch decode, records=$RECORD_COUNT, " +
                "ns_per_batch=${format(medianNs)}, ns_per_record=${format(perRecordNs)}",
        )
    }

    private fun batch(recordCount: Int): ByteBuffer {
        val size = ArtTiNativeProtocol.BATCH_HEADER_SIZE + recordCount * ArtTiNativeProtocol.RECORD_SIZE
        return ByteBuffer.allocate(size).order(ByteOrder.LITTLE_ENDIAN).apply {
            putInt(ArtTiNativeProtocol.BATCH_MAGIC)
            putShort(ArtTiNativeProtocol.PROTOCOL_VERSION.toShort())
            putShort(ArtTiNativeProtocol.BATCH_HEADER_SIZE.toShort())
            putInt(size)
            putInt(recordCount)
            putLong(1L)
            putLong(recordCount.toLong())
            repeat(recordCount) { index ->
                putInt(ArtTiNativeProtocol.RECORD_SIZE)
                putShort(6)
                putShort(1)
                putInt(0)
                putInt(0)
                putLong(index + 1L)
                repeat(8) { putLong(index.toLong()) }
            }
            flip()
        }
    }

    private fun format(value: Double): String = String.format(Locale.US, "%.1f", value)

    private companion object {
        const val RECORD_COUNT = 256
        const val WARMUP_SAMPLES = 3
        const val SAMPLES = 7
        const val ITERATIONS_PER_SAMPLE = 10_000

        @Volatile
        var benchmarkSink = 0L
    }
}
