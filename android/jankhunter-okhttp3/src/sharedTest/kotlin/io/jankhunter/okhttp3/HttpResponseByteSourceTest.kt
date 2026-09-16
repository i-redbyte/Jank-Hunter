package io.jankhunter.okhttp3

import java.io.IOException
import okio.Buffer
import okio.Source
import okio.Timeout
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class HttpResponseByteSourceTest {
    @Test
    fun http1ObservesTheFirstPositiveReadOnceWithoutConsumingOrCopyingBytes() {
        val clock = Clock()
        val callback = Observation(clock)
        val raw = Chunks(clock, listOf(600L to byteArrayOf(72), 1100L to "TTP/1.1".toByteArray()))
        val source = HttpResponseByteSource(raw)
        assertTrue(source.armHttp1(callback))
        val received = Buffer()
        assertEquals(1L, source.read(received, 8192L))
        assertEquals(listOf(600L), callback.times)
        source.read(received, 8192L)
        assertEquals("HTTP/1.1", received.readUtf8())
        assertEquals(listOf(600L), callback.times)
        assertEquals(1, clock.reads)
    }

    @Test
    fun fragmentedHttp2HeaderKeepsTheTimestampOfItsFirstByte() {
        val clock = Clock()
        val frame = frame(1, 3, 0)
        val raw = Chunks(clock, listOf(600L to frame.copyOfRange(0, 1), 1100L to frame.copyOfRange(1, 9)))
        val source = HttpResponseByteSource(raw)
        source.useHttp2()
        val callback = Observation(clock)
        assertTrue(source.armStream(3, callback))
        val received = Buffer()
        source.read(received, 8192L)
        assertTrue(callback.times.isEmpty())
        source.read(received, 8192L)
        assertEquals(listOf(600L), callback.times)
        assertEquals(frame.toList(), received.readByteArray().toList())
    }

    @Test
    fun multiplexedRepliesUseStreamIdentityEvenWhenFramesShareOneRead() {
        val clock = Clock()
        val payload = frame(4, 0, 0) + frame(1, 5, 3) + byteArrayOf(1, 2, 3) + frame(1, 3, 0)
        val source = HttpResponseByteSource(Chunks(clock, listOf(700L to payload)))
        source.useHttp2()
        val first = Observation(clock)
        val second = Observation(clock)
        assertTrue(source.armStream(3, first))
        assertTrue(source.armStream(5, second))
        val received = Buffer()
        assertEquals(payload.size.toLong(), source.read(received, 8192L))
        assertEquals(listOf(700L), first.times)
        assertEquals(listOf(700L), second.times)
        assertEquals(payload.toList(), received.readByteArray().toList())
    }

    @Test
    fun dataAndPushStreamsCannotConsumeAnUnrelatedResponseObservation() {
        val clock = Clock()
        val source = HttpResponseByteSource(Chunks(clock, listOf(
            500L to (frame(0, 3, 4) + byteArrayOf(0, 1, 0, 3) + frame(1, 2, 0)),
            900L to frame(1, 3, 0),
        )))
        source.useHttp2()
        val callback = Observation(clock)
        source.armStream(3, callback)
        source.read(Buffer(), 8192L)
        assertTrue(callback.times.isEmpty())
        source.read(Buffer(), 8192L)
        assertEquals(listOf(900L), callback.times)
    }

    @Test
    fun capacityRefusalDoesNotOverwriteLiveStreamsAndReleasedSlotsAreReusable() {
        val clock = Clock()
        val source = HttpResponseByteSource(Chunks(clock, listOf(800L to (frame(1, 5, 0) + frame(1, 7, 0)))), 2)
        source.useHttp2()
        val cancelled = Observation(clock)
        val retained = Observation(clock)
        val replacement = Observation(clock)
        assertTrue(source.armStream(3, cancelled))
        assertTrue(source.armStream(5, retained))
        assertFalse(source.armStream(7, replacement))
        source.disarm(3, cancelled)
        assertTrue(source.armStream(7, replacement))
        source.disarm(7, cancelled)
        source.read(Buffer(), 8192L)
        assertTrue(cancelled.times.isEmpty())
        assertEquals(listOf(800L), retained.times)
        assertEquals(listOf(800L), replacement.times)
    }

    @Test
    fun disabledObservationKeepsFramingWithoutReadingTheClock() {
        val clock = Clock()
        val source = HttpResponseByteSource(Chunks(clock, listOf(
            400L to (frame(0, 1, 4) + byteArrayOf(1)),
            700L to (byteArrayOf(2, 3, 4) + frame(1, 3, 0)),
        )))
        source.useHttp2()
        source.read(Buffer(), 8192L)
        assertEquals(0, clock.reads)
        val callback = Observation(clock)
        source.armStream(3, callback)
        source.read(Buffer(), 8192L)
        assertEquals(listOf(700L), callback.times)
    }

    @Test
    fun repeatedCollisionsAndDeletionsNeverMoveAReplyToAnotherStream() {
        val clock = Clock()
        val incoming = Buffer()
        val source = HttpResponseByteSource(incoming, 32)
        source.useHttp2()
        val random = java.util.Random(813_271L)
        val live = mutableMapOf<Int, Observation>()
        repeat(4_000) { step ->
            val id = random.nextInt(256) * 2 + 1
            when (random.nextInt(3)) {
                0 -> {
                    val candidate = Observation(clock)
                    val expected = id !in live && live.size < 32
                    assertEquals(expected, source.armStream(id, candidate))
                    if (expected) live[id] = candidate
                }
                1 -> live.remove(id)?.let { source.disarm(id, it) }
                else -> {
                    clock.now = step.toLong()
                    incoming.write(frame(1, id, 0))
                    assertEquals(9L, source.read(Buffer(), 8192L))
                    live.remove(id)?.let { assertEquals(listOf(step.toLong()), it.times) }
                    assertTrue(live.values.all { it.times.isEmpty() })
                }
            }
        }
        source.close()
        assertFalse(source.armStream(1, Observation(clock)))
    }

    @Test
    fun observationFailureCannotChangeBytesOrDelegateIOException() {
        val clock = Clock()
        val failure = IOException("original transport failure")
        val source = HttpResponseByteSource(object : Source {
            var first = true
            override fun read(sink: Buffer, byteCount: Long): Long {
                if (!first) throw failure
                first = false
                sink.writeByte(42)
                return 1L
            }
            override fun timeout(): Timeout = Timeout.NONE
            override fun close() = Unit
        })
        source.armHttp1(object : HttpFirstByteObservation {
            override val firstByteClock: NetworkLongSource = clock
            override fun onFirstByte(atMs: Long) { error("telemetry failed") }
        })
        val sink = Buffer()
        assertEquals(1L, source.read(sink, 8192L))
        assertEquals(42, sink.readByte().toInt())
        try {
            source.read(sink, 8192L)
            throw AssertionError("transport IOException was swallowed")
        } catch (caught: IOException) { assertSame(failure, caught) }
    }

    private class Clock : NetworkLongSource {
        var now = 0L
        var reads = 0
        override fun getAsLong(): Long { reads++; return now }
    }
    private class Observation(override val firstByteClock: NetworkLongSource) : HttpFirstByteObservation {
        val times = mutableListOf<Long>()
        override fun onFirstByte(atMs: Long) { times += atMs }
    }
    private class Chunks(val clock: Clock, chunks: List<Pair<Long, ByteArray>>) : Source {
        private val remaining = chunks.iterator()
        override fun read(sink: Buffer, byteCount: Long): Long {
            if (!remaining.hasNext()) return -1L
            val (time, bytes) = remaining.next()
            check(bytes.size <= byteCount)
            clock.now = time
            sink.write(bytes)
            return bytes.size.toLong()
        }
        override fun timeout(): Timeout = Timeout.NONE
        override fun close() = Unit
    }
    private fun frame(type: Int, id: Int, size: Int): ByteArray = byteArrayOf(
        (size ushr 16).toByte(), (size ushr 8).toByte(), size.toByte(), type.toByte(), 4,
        (id ushr 24).toByte(), (id ushr 16).toByte(), (id ushr 8).toByte(), id.toByte(),
    )
}
