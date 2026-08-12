package io.jankhunter.runtime

/** Semantic event types shared by optional runtime integrations and storage adapters. */
object JankHunterAgentEventType {
    const val AGENT_STATUS = 1
    const val AGENT_CAPABILITY = 2
    const val AGENT_QUALITY_SNAPSHOT = 3
    const val THREAD_START = 4
    const val THREAD_END = 5
    const val GC_INTERVAL = 6
    const val MONITOR_CONTENTION_INTERVAL = 7
    const val THREAD_STACK_SAMPLE = 8
    const val STACK_DEFINITION = 9
    const val CLOCK_SYNC = 10
    const val CORRELATION_LINK = 11
    const val METHOD_DEFINITION = 12

    internal const val MIN = AGENT_STATUS
    internal const val MAX = METHOD_DEFINITION
}

object JankHunterAgentEventFlag {
    /** Correlation record defines contextToken -> envelope attribution, rather than thread -> token. */
    const val CONTEXT_DEFINITION = 1
}

/**
 * Reusable fixed-capacity semantic batch.
 *
 * It stores records in one packed [LongArray], so a native drain creates no JVM object per event.
 * The sink must copy or consume the batch before [tryPublish] returns; callers may immediately
 * clear and reuse it.
 */
class JankHunterAgentEventBatch(capacity: Int) {
    private val words: LongArray

    val capacity: Int
    var size: Int = 0
        private set

    init {
        require(capacity in 1..MAX_CAPACITY) { "Agent event batch capacity is out of bounds" }
        this.capacity = capacity
        words = LongArray(capacity * WORDS_PER_EVENT)
    }

    fun clear() {
        size = 0
    }

    @Suppress("LongParameterList")
    fun tryAppend(
        type: Int,
        schemaVersion: Int,
        flags: Int,
        producerSequence: Long,
        monotonicNs: Long,
        producerId: Long,
        threadToken: Long,
        contextToken: Long,
        payload0: Long,
        payload1: Long,
        payload2: Long,
        payload3: Long,
    ): Boolean {
        if (size >= capacity || type !in 1..MAX_WIRE_TYPE || schemaVersion !in 1..MAX_SCHEMA_VERSION ||
            monotonicNs < 0L
        ) {
            return false
        }
        val offset = size * WORDS_PER_EVENT
        words[offset + TYPE_SCHEMA_FLAGS] = packHeader(type, schemaVersion, flags)
        words[offset + PRODUCER_SEQUENCE] = producerSequence
        words[offset + MONOTONIC_NS] = monotonicNs
        words[offset + PRODUCER_ID] = producerId
        words[offset + THREAD_TOKEN] = threadToken
        words[offset + CONTEXT_TOKEN] = contextToken
        words[offset + PAYLOAD_0] = payload0
        words[offset + PAYLOAD_1] = payload1
        words[offset + PAYLOAD_2] = payload2
        words[offset + PAYLOAD_3] = payload3
        size++
        return true
    }

    fun type(index: Int): Int = (header(index) and UINT16_MASK).toInt()

    fun schemaVersion(index: Int): Int = ((header(index) ushr 16) and UINT16_MASK).toInt()

    fun flags(index: Int): Int = (header(index) ushr 32).toInt()

    fun producerSequence(index: Int): Long = word(index, PRODUCER_SEQUENCE)

    fun monotonicNs(index: Int): Long = word(index, MONOTONIC_NS)

    fun producerId(index: Int): Long = word(index, PRODUCER_ID)

    fun threadToken(index: Int): Long = word(index, THREAD_TOKEN)

    fun contextToken(index: Int): Long = word(index, CONTEXT_TOKEN)

    fun payload0(index: Int): Long = word(index, PAYLOAD_0)

    fun payload1(index: Int): Long = word(index, PAYLOAD_1)

    fun payload2(index: Int): Long = word(index, PAYLOAD_2)

    fun payload3(index: Int): Long = word(index, PAYLOAD_3)

    internal fun copyPackedWords(): LongArray = words.copyOf(size * WORDS_PER_EVENT)

    private fun header(index: Int): Long = word(index, TYPE_SCHEMA_FLAGS)

    private fun word(index: Int, field: Int): Long {
        require(index in 0 until size) { "Agent event index is out of bounds" }
        return words[index * WORDS_PER_EVENT + field]
    }

    internal companion object {
        const val WORDS_PER_EVENT = 10
        const val TYPE_SCHEMA_FLAGS = 0
        const val PRODUCER_SEQUENCE = 1
        const val MONOTONIC_NS = 2
        const val PRODUCER_ID = 3
        const val THREAD_TOKEN = 4
        const val CONTEXT_TOKEN = 5
        const val PAYLOAD_0 = 6
        const val PAYLOAD_1 = 7
        const val PAYLOAD_2 = 8
        const val PAYLOAD_3 = 9
        private const val MAX_CAPACITY = 4_096
        private const val MAX_WIRE_TYPE = 0xFFFF
        private const val MAX_SCHEMA_VERSION = 0xFFFF
        private const val UINT16_MASK = 0xFFFFL

        private fun packHeader(type: Int, schemaVersion: Int, flags: Int): Long {
            return (type.toLong() and UINT16_MASK) or
                ((schemaVersion.toLong() and UINT16_MASK) shl 16) or
                (flags.toLong() shl 32)
        }
    }
}

/** Non-blocking storage-neutral boundary for optional ART TI/native integrations. */
interface JankHunterAgentEventSink {
    fun tryPublish(batch: JankHunterAgentEventBatch): Boolean

    fun tryPublishContext(
        contextToken: Long,
        screen: String?,
        owner: String?,
        flow: String?,
        step: String?,
    ): Boolean

    fun tryPublishMethodDefinition(methodId: Long, symbol: String): Boolean
}
