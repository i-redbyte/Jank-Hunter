package io.jankhunter.okhttp3

import okio.Buffer
import okio.Source
import okio.Timeout

/** Internal callback only: it never forwards a synthetic event to the application's EventListener. */
internal interface HttpFirstByteObservation {
    val firstByteClock: NetworkLongSource
    fun onFirstByte(atMs: Long)
}

/**
 * Observes an existing plaintext read without an extra read, buffer, payload copy or HPACK decoder.
 * HTTP/2 parses only nine frame-header bytes and skips payload by length. A fragmented header keeps
 * the time of its first fragment; multiplexed replies use actual stream IDs registered before send.
 * The production socket Source appends at most one Okio segment per read, so accesses to its newly
 * appended tail have bounded segment traversal. Source reads have one consumer. Registrations may
 * be concurrent; callbacks run outside the lock.
 */
internal class HttpResponseByteSource(
    private val delegate: Source,
    private val maxStreams: Int = 1_024,
) : Source {
    private var streams: FirstByteStreams? = null
    private var http1: HttpFirstByteObservation? = null
    @Volatile private var http2 = false
    private var closed = false
    private var invalid = false
    private var headerBytes = 0
    private var frameLength = 0
    private var frameType = 0
    private var frameStream = 0
    private var frameFirstAt = UNKNOWN_TIME
    private var payloadRemaining = 0
    @Volatile private var pending = 0
    @Volatile private var clock: NetworkLongSource? = null

    init { require(maxStreams in 1..65_536 && maxStreams and (maxStreams - 1) == 0) }

    @Synchronized
    fun armHttp1(observation: HttpFirstByteObservation): Boolean {
        if (closed || invalid || http2 || http1 != null) return false
        http1 = observation
        clock = observation.firstByteClock
        pending = 1
        return true
    }

    @Synchronized
    fun useHttp2() {
        if (http2) return
        check(http1 == null)
        http2 = true
    }

    @Synchronized
    fun armStream(id: Int, observation: HttpFirstByteObservation): Boolean {
        if (closed || invalid || !http2 || id <= 0) return false
        val table = streams ?: FirstByteStreams(maxStreams).also { streams = it }
        if (!table.put(id, observation)) return false
        if (pending == 0) clock = observation.firstByteClock
        pending = table.size
        return true
    }

    @Synchronized
    fun disarm(id: Int, expected: HttpFirstByteObservation) {
        if (id == 0 && http1 === expected) http1 = null
        if (id > 0) streams?.remove(id, expected)
        refreshPending()
    }

    override fun read(sink: Buffer, byteCount: Long): Long {
        // The delegate's result and IOException belong to the application and must remain unchanged.
        val count = delegate.read(sink, byteCount)
        if (count <= 0L) return count
        // HTTP/1 has no shared frame alignment to maintain after its observation is consumed.
        if (pending == 0 && !http2) return count
        val at = readTime()
        try {
            observe(sink, sink.size() - count, count, at)
        } catch (failure: Throwable) {
            rethrowFatal(failure)
            synchronized(this) { invalid = true; clearObservations() }
        }
        return count
    }

    private fun readTime(): Long {
        if (pending == 0) return UNKNOWN_TIME
        return try { clock?.getAsLong() ?: UNKNOWN_TIME } catch (failure: Throwable) {
            rethrowFatal(failure)
            UNKNOWN_TIME
        }
    }

    private fun observe(buffer: Buffer, from: Long, count: Long, readAt: Long) {
        var offset = from
        val end = from + count
        while (offset < end) {
            var observation: HttpFirstByteObservation? = null
            var firstAt = UNKNOWN_TIME
            synchronized(this) {
                if (closed || invalid) return
                if (!http2) {
                    observation = http1
                    http1 = null
                    firstAt = readAt
                    offset = end
                    refreshPending()
                } else if (payloadRemaining > 0) {
                    val skipped = minOf(end - offset, payloadRemaining.toLong()).toInt()
                    offset += skipped
                    payloadRemaining -= skipped
                } else {
                    while (headerBytes < FRAME_HEADER_BYTES && offset < end) {
                        val value = buffer.getByte(offset++).toInt() and 255
                        if (headerBytes == 0) {
                            frameFirstAt = readAt
                            frameLength = 0
                            frameStream = 0
                        }
                        when (headerBytes) {
                            in 0..2 -> frameLength = (frameLength shl 8) or value
                            3 -> frameType = value
                            in 5..8 -> frameStream = (frameStream shl 8) or value
                        }
                        headerBytes++
                    }
                    if (headerBytes == FRAME_HEADER_BYTES) {
                        payloadRemaining = frameLength
                        headerBytes = 0
                        if (frameType == HEADERS_FRAME) {
                            observation = streams?.remove(frameStream and Int.MAX_VALUE)
                            firstAt = frameFirstAt
                            refreshPending()
                        }
                    }
                }
            }
            observation?.let { notify(it, firstAt) }
        }
    }

    private fun notify(observation: HttpFirstByteObservation, at: Long) {
        try { observation.onFirstByte(at) } catch (failure: Throwable) { rethrowFatal(failure) }
    }

    private fun refreshPending() {
        pending = (streams?.size ?: 0) + if (http1 == null) 0 else 1
        if (pending == 0) clock = null
    }

    private fun clearObservations() {
        http1 = null
        streams = null
        pending = 0
        clock = null
    }

    override fun timeout(): Timeout = delegate.timeout()

    override fun close() {
        try { delegate.close() } finally { synchronized(this) { closed = true; clearObservations() } }
    }

    private companion object {
        const val FRAME_HEADER_BYTES = 9
        const val HEADERS_FRAME = 1
        const val UNKNOWN_TIME = Long.MIN_VALUE
        fun rethrowFatal(failure: Throwable) {
            if (failure is VirtualMachineError || failure is ThreadDeath) throw failure
        }
    }
}

/** Primitive open addressing at <= 50% load, with backward-shift deletion and no entry objects. */
private class FirstByteStreams(private val limit: Int) {
    private var keys = IntArray(0)
    private var values = arrayOfNulls<HttpFirstByteObservation>(0)
    var size = 0
        private set

    init { require(limit in 1..65_536 && limit and (limit - 1) == 0) }

    fun put(key: Int, value: HttpFirstByteObservation): Boolean {
        if (keys.isNotEmpty()) {
            val existing = locate(key)
            if (keys[existing] == key) return values[existing] === value
        }
        if (size == limit) return false
        if (keys.isEmpty() || (size + 1) * 2 > keys.size) grow()
        val slot = locate(key)
        keys[slot] = key
        values[slot] = value
        size++
        return true
    }

    fun remove(key: Int, expected: HttpFirstByteObservation? = null): HttpFirstByteObservation? {
        if (keys.isEmpty() || key <= 0) return null
        var hole = locate(key)
        if (keys[hole] != key || (expected != null && values[hole] !== expected)) return null
        val removed = values[hole]
        val mask = keys.lastIndex
        var next = (hole + 1) and mask
        while (keys[next] != 0) {
            val home = hash(keys[next]) and mask
            if (((hole - home) and mask) < ((next - home) and mask)) {
                keys[hole] = keys[next]
                values[hole] = values[next]
                hole = next
            }
            next = (next + 1) and mask
        }
        keys[hole] = 0
        values[hole] = null
        size--
        return removed
    }

    private fun grow() {
        val capacity = if (keys.isEmpty()) minOf(16, limit * 2) else keys.size * 2
        val nextKeys = IntArray(capacity)
        val nextValues = arrayOfNulls<HttpFirstByteObservation>(capacity)
        val mask = capacity - 1
        for (index in keys.indices) {
            val key = keys[index]
            if (key == 0) continue
            var slot = hash(key) and mask
            while (nextKeys[slot] != 0) slot = (slot + 1) and mask
            nextKeys[slot] = key
            nextValues[slot] = values[index]
        }
        keys = nextKeys
        values = nextValues
    }

    private fun locate(key: Int): Int {
        val mask = keys.lastIndex
        var index = hash(key) and mask
        while (keys[index] != 0 && keys[index] != key) index = (index + 1) and mask
        return index
    }

    private fun hash(key: Int): Int {
        val mixed = (key xor (key ushr 16)) * -1_640_531_527
        return mixed xor (mixed ushr 16)
    }
}
