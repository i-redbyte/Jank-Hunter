package io.jankhunter.runtime.internal.io

import android.os.Process
import android.os.SystemClock
import java.util.concurrent.ThreadLocalRandom
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicLongArray

internal object Jhlog {
    const val FORMAT_MARKER = 0x81
    const val FORMAT_MAJOR = 2
    const val FORMAT_MINOR = 0
    const val FORMAT_PATCH = 0
    const val FORMAT_VERSION = "2.0.0"
    const val HEADER_SCHEMA = 1L
    const val MAX_FILE_HEADER_BYTES = 4 * 1024
    const val TARGET_RAW_CHUNK_BYTES = 64 * 1024
    const val MAX_RAW_CHUNK_BYTES = 256 * 1024
    const val CHUNK_HEADER_BYTES = 32
    const val COMMIT_TRAILER_BYTES = 20

    const val REQUIRED_FEATURES = 0x1fffL
    const val OPTIONAL_FEATURES = 0x01L

    const val FEATURE_CHUNK_CRC_COMMIT = 1L shl 0
    const val FEATURE_LENGTH_DELIMITED_RECORDS = 1L shl 1
    const val FEATURE_SYMBOL_REFERENCES = 1L shl 2
    const val FEATURE_PRODUCER_METADATA = 1L shl 3
    const val FEATURE_CHUNK_LOCAL_CONTEXT = 1L shl 4
    const val FEATURE_QUALITY_RECORDS = 1L shl 5
    const val FEATURE_EMBEDDED_STABLE_SYMBOLS = 1L shl 6
    const val FEATURE_LOG_GROWTH_RECORDS = 1L shl 7
    const val FEATURE_EXACT_EVENT_ADMISSION = 1L shl 8
    const val FEATURE_PROCESS_SCOPE = 1L shl 9
    const val FEATURE_COLUMNAR_RUNTIME_CALLS = 1L shl 10
    const val FEATURE_SEGMENT_DIGEST_CHAIN = 1L shl 11
    const val FEATURE_PROCESS_ROSTER = 1L shl 12
    const val OPTIONAL_FEATURE_GZIP_CHUNKS = 1L shl 0
    const val BEST_EFFORT_FEATURES = REQUIRED_FEATURES and FEATURE_EXACT_EVENT_ADMISSION.inv()

    const val PROCESS_SCOPE_ALL = 1L
    const val PROCESS_SCOPE_MAIN_ONLY = 2L
    const val PROCESS_SCOPE_ALLOWLIST = 3L

    const val CHUNK_FLAG_GZIP = 1 shl 0
    const val CHUNK_FLAG_FINAL = 1 shl 1

    const val ENVELOPE_HAS_TIME = 1L shl 0
    const val ENVELOPE_HAS_THREAD = 1L shl 1
    const val ENVELOPE_HAS_CONTEXT = 1L shl 2
    const val ENVELOPE_SAME_CONTEXT = 1L shl 3
    const val ENVELOPE_HAS_ATTRIBUTES = 1L shl 4

    const val CONTEXT_SCREEN = 1L shl 0
    const val CONTEXT_OWNER = 1L shl 1
    const val CONTEXT_FLOW = 1L shl 2
    const val CONTEXT_STEP = 1L shl 3

    const val TYPE_DICTIONARY = 1
    const val TYPE_SESSION = 2
    const val TYPE_DEVICE_CONTEXT = 3
    const val TYPE_HTTP = 4
    const val TYPE_UI_WINDOW = 5
    const val TYPE_STALL = 6
    const val TYPE_MEMORY = 7
    const val TYPE_RETAINED = 8
    const val TYPE_COUNTER = 9
    const val TYPE_GAUGE = 10
    // Type 11 (continuous FLOW_TRANSITION) is retired. Useful events carry
    // screen/owner/flow/step attribution atomically in their envelope.
    const val TYPE_LOG_SPAM = 12
    const val TYPE_PROBLEM = 13
    const val TYPE_RUNTIME_CALL = 14
    const val TYPE_QUALITY_SNAPSHOT = 15
    const val TYPE_SEGMENT_END = 16
    const val TYPE_LOG_GROWTH = 17
    const val TYPE_PROCESS_EXIT = 18
    const val TYPE_IO = 19

    const val COLLECTOR_FPS = 1L shl 0
    const val COLLECTOR_JANKSTATS = 1L shl 1
    const val COLLECTOR_PROCESS_EXIT = 1L shl 2
    const val COLLECTOR_IO_TRACING = 1L shl 3
    const val COLLECTOR_SYSTEM_SAMPLER = 1L shl 4
    const val COLLECTOR_MAIN_THREAD_STALLS = 1L shl 5
    const val COLLECTOR_RETAINED_OBJECTS = 1L shl 6
    const val COLLECTOR_COMPOSE = 1L shl 7
    const val COLLECTOR_ROOM = 1L shl 8
    const val COLLECTOR_WORKER = 1L shl 9
    const val COLLECTOR_KNOWN_MASK = COLLECTOR_FPS or COLLECTOR_JANKSTATS or
        COLLECTOR_PROCESS_EXIT or COLLECTOR_IO_TRACING or COLLECTOR_SYSTEM_SAMPLER or
        COLLECTOR_MAIN_THREAD_STALLS or COLLECTOR_RETAINED_OBJECTS or COLLECTOR_COMPOSE or
        COLLECTOR_ROOM or COLLECTOR_WORKER

    const val UI_SOURCE_JANKSTATS = 1L
    const val UI_SOURCE_CHOREOGRAPHER = 2L
    val UI_FRAME_HISTOGRAM_UPPER_US = longArrayOf(
        8_000L,
        12_000L,
        16_000L,
        20_000L,
        24_000L,
        32_000L,
        40_000L,
        50_000L,
        67_000L,
        100_000L,
        250_000L,
        1_000_000L,
    )
    const val UI_FRAME_HISTOGRAM_BUCKET_COUNT = 13

    const val IO_FILE_READ = 1L
    const val IO_FILE_WRITE = 2L
    const val IO_FILE_SYNC = 3L
    const val IO_DATABASE_READ = 4L
    const val IO_DATABASE_WRITE = 5L
    const val IO_CONTENT_READ = 6L
    const val IO_CONTENT_WRITE = 7L

    const val MAX_RUNTIME_CALL_BLOCK_ROWS = 128

    const val LOG_GROWTH_HISTORY = 1L
    const val LOG_GROWTH_LIVE = 2L

    const val SEGMENT_END_NORMAL = 0L
    const val SEGMENT_END_SIZE_LIMIT = 1L
    const val SEGMENT_END_IO_ERROR = 2L
    const val SEGMENT_END_SHUTDOWN = 3L
    const val SEGMENT_END_ROTATION = 4L
    const val SEGMENT_END_STORAGE_BUDGET = 5L

    val FILE_MAGIC = byteArrayOf(
        'J'.code.toByte(),
        'H'.code.toByte(),
        'L'.code.toByte(),
        'O'.code.toByte(),
        'G'.code.toByte(),
        '\r'.code.toByte(),
        '\n'.code.toByte(),
        FORMAT_MARKER.toByte(),
        FORMAT_MAJOR.toByte(),
        FORMAT_MINOR.toByte(),
        FORMAT_PATCH.toByte(),
    )
    val CHUNK_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), '1'.code.toByte())
    val COMMIT_MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'C'.code.toByte(), 'M'.code.toByte())
}

internal object QualityCounterId {
    const val ACCEPTED_EVENT_TOTAL = 1
    const val WRITTEN_EVENT_TOTAL = 2
    const val QUEUE_FULL_TOTAL = 3
    const val NOT_ACCEPTING_TOTAL = 4
    const val CONTROL_LANE_FULL_TOTAL = 5
    const val CONTROL_TIMEOUT_TOTAL = 6
    const val CONTROL_INTERRUPTED_TOTAL = 7
    const val WRITER_IO_ERROR_TOTAL = 8
    const val EVENT_LOST_AFTER_IO_TOTAL = 9
    const val DICTIONARY_OVERFLOW_TOTAL = 10
    const val DICTIONARY_VALUE_TRUNCATED_TOTAL = 11
    const val OVERSIZED_RECORD_TOTAL = 12
    const val COMMITTED_CHUNK_TOTAL = 13
    const val FAILED_CHUNK_TOTAL = 14
    const val CLOSE_TIMEOUT_TOTAL = 16
    const val EVENT_LOST_AFTER_SIZE_LIMIT_TOTAL = 17
    const val WRITER_ADMISSION_CONTENTION_TOTAL = 18
    const val EVENT_LOST_AFTER_STORAGE_BUDGET_TOTAL = 19

    const val METRIC_CARDINALITY_LOSS = 0x2000
    const val INVALID_METRIC = 0x2001
    const val RUNTIME_STACK_MISMATCH = 0x2003
    const val LOG_SPAM_CARDINALITY_LOSS = 0x2004
    const val HANDLER_ENTRY_LIMIT = 0x2005
    const val HANDLER_WRAPPER_LIMIT = 0x2006
    const val LIFECYCLE_REGISTRY_LIMIT = 0x2007
    const val OBJECT_WATCHER_LIMIT = 0x2008
    const val JANKSTATS_HANDLE_LIMIT = 0x2009
    const val METRIC_FLUSH_TIMEOUT = 0x200a
    const val RUNTIME_GRAPH_SHUTDOWN_LOSS = 0x200f
    const val RUNTIME_GRAPH_WRITER_REJECTION_LOSS = 0x2010
    const val HANDLER_CONTENTION_BYPASS = 0x2013
    const val RUNTIME_EVENT_BUFFER_CAPACITY_LOSS = 0x2017
    const val RUNTIME_EVENT_REGISTRY_CAPACITY_LOSS = 0x2018
    const val METHOD_COUNTER_CARDINALITY_LOSS = 0x2019
    const val RUNTIME_EVENT_WRITER_REJECTION_LOSS = 0x201a
    const val RUNTIME_GRAPH_INPUT_TOTAL = 0x201b
    const val RUNTIME_GRAPH_EMITTED_TOTAL = 0x201c
    const val RUNTIME_GRAPH_BACKPRESSURE_COUNT = 0x201f
    const val RUNTIME_GRAPH_BACKPRESSURE_NANOS = 0x2020
    const val WRITER_BACKPRESSURE_COUNT = 0x2021
    const val WRITER_BACKPRESSURE_NANOS = 0x2022
    const val RUNTIME_EVENT_BACKPRESSURE_COUNT = 0x2023
    const val RUNTIME_EVENT_BACKPRESSURE_NANOS = 0x2024
    const val RUNTIME_GRAPH_DISABLED = 0x2025
    const val RUNTIME_HOOK_FAILURE_TOTAL = 0x2026
    const val ARCHIVE_EVICTED_RUN_TOTAL = 0x2027
    const val ARCHIVE_EVICTED_SEGMENT_TOTAL = 0x2028
    const val ARCHIVE_EVICTED_BYTES_TOTAL = 0x2029
    const val RUNTIME_HOOK_INSTRUMENTATION_FAILURE = 0x202a
    const val RUNTIME_HOOK_ASYNC_WRAPPER_FAILURE = 0x202b
    const val RUNTIME_HOOK_LIFECYCLE_FAILURE = 0x202c
    const val RUNTIME_HOOK_COLLECTOR_FAILURE = 0x202d
    const val RUNTIME_HOOK_CONTEXT_FAILURE = 0x202e
    const val RUNTIME_HOOK_SCHEDULER_FAILURE = 0x202f
    const val JANKSTATS_DEPENDENCY_MISSING = 0x2030
    const val JANKSTATS_INSTALL_FAILURE = 0x2031
    const val JANKSTATS_FRAME_FAILURE = 0x2032
    const val JANKSTATS_CONTROL_FAILURE = 0x2033
    const val RUNTIME_HOOK_UNCLASSIFIED_FAILURE = 0x2034

    const val REASON_QUEUE_FULL = 1
    const val REASON_NOT_ACCEPTING = 2
    const val REASON_IO_LOST = 3
    const val REASON_OVERSIZED = 4
    const val REASON_SIZE_LIMIT = 5
    const val REASON_ADMISSION_CONTENTION = 6
    const val REASON_STORAGE_BUDGET = 7

    fun eventReason(recordType: Int, reason: Int): Int {
        return 0x1000 + recordType.coerceAtLeast(0) * 16 + reason
    }
}

internal class LogQualityCounters {
    private val values = AtomicLongArray(MAX_COUNTER_ID + 1)
    private val generation = AtomicLong()
    private val snapshotSequence = AtomicLong()
    private val frozen = AtomicBoolean()
    private val activeUpdates = AtomicInteger()

    fun add(counterId: Int, delta: Long = 1L) {
        if (counterId !in 1..MAX_COUNTER_ID || delta <= 0L) return
        update {
            values.addAndGet(counterId, delta)
            generation.incrementAndGet()
        }
    }

    fun subtractHousekeeping(counterId: Int, delta: Long = 1L) {
        if (counterId !in 1..MAX_COUNTER_ID || delta <= 0L) return
        updateHousekeeping {
            values.addAndGet(counterId, -delta)
        }
    }

    /** Internal writer state may advance after producer counters have been frozen for sealing. */
    fun addHousekeeping(counterId: Int, delta: Long = 1L) {
        if (counterId !in 1..MAX_COUNTER_ID || delta <= 0L) return
        updateHousekeeping {
            values.addAndGet(counterId, delta)
        }
    }

    fun addAccepted(delta: Long = 1L) = add(QualityCounterId.ACCEPTED_EVENT_TOTAL, delta)

    fun addRejected(recordType: Int, reason: Int, delta: Long = 1L) {
        if (delta <= 0L) return
        val aggregateId = when (reason) {
            QualityCounterId.REASON_QUEUE_FULL -> QualityCounterId.QUEUE_FULL_TOTAL
            QualityCounterId.REASON_NOT_ACCEPTING -> QualityCounterId.NOT_ACCEPTING_TOTAL
            QualityCounterId.REASON_IO_LOST -> QualityCounterId.EVENT_LOST_AFTER_IO_TOTAL
            QualityCounterId.REASON_OVERSIZED -> QualityCounterId.OVERSIZED_RECORD_TOTAL
            QualityCounterId.REASON_SIZE_LIMIT -> QualityCounterId.EVENT_LOST_AFTER_SIZE_LIMIT_TOTAL
            QualityCounterId.REASON_STORAGE_BUDGET -> QualityCounterId.EVENT_LOST_AFTER_STORAGE_BUDGET_TOTAL
            else -> 0
        }
        val eventId = QualityCounterId.eventReason(recordType, reason)
        update {
            if (aggregateId != 0) values.addAndGet(aggregateId, delta)
            if (eventId in 1..MAX_COUNTER_ID) values.addAndGet(eventId, delta)
            generation.incrementAndGet()
        }
    }

    fun generation(): Long = generation.get()

    fun value(counterId: Int): Long = if (counterId in 1..MAX_COUNTER_ID) values.get(counterId) else 0L

    fun nextSnapshotSequence(): Long = snapshotSequence.incrementAndGet()

    /**
     * Establishes the clean session boundary without serializing hot producers on a monitor.
     * An updater either reserves before the boundary and is awaited, or observes frozen and exits.
     */
    fun freeze() {
        frozen.set(true)
        while (activeUpdates.get() != 0) {
            Thread.yield()
        }
    }

    fun snapshot(): List<QualityEntry> {
        while (true) {
            while (activeUpdates.get() != 0) Thread.yield()
            val generationBefore = generation.get()
            if (activeUpdates.get() != 0) continue

            val entries = ArrayList<QualityEntry>()
            for (counterId in 1..MAX_COUNTER_ID) {
                val value = values.get(counterId)
                if (value > 0L) entries += QualityEntry(counterId, value)
            }

            val generationAfter = generation.get()
            val updatesAfter = activeUpdates.get()
            if (generationBefore == generationAfter && updatesAfter == 0) return entries
        }
    }

    data class QualityEntry(
        val counterId: Int,
        val value: Long,
    )

    private inline fun update(block: () -> Unit) {
        if (frozen.get()) return
        activeUpdates.incrementAndGet()
        if (frozen.get()) {
            activeUpdates.decrementAndGet()
            return
        }
        try {
            block()
        } finally {
            activeUpdates.decrementAndGet()
        }
    }

    private inline fun updateHousekeeping(block: () -> Unit) {
        activeUpdates.incrementAndGet()
        try {
            block()
            generation.incrementAndGet()
        } finally {
            activeUpdates.decrementAndGet()
        }
    }

    private companion object {
        const val MAX_COUNTER_ID = 0x20ff
    }
}

internal data class LogEventContext(
    val screen: String?,
    val owner: String?,
    val flow: String?,
    val step: String?,
) {
    fun matches(screen: String?, owner: String?, flow: String?, step: String?): Boolean {
        return this.screen == normalized(screen) &&
            this.owner == normalized(owner) &&
            this.flow == normalized(flow) &&
            this.step == normalized(step)
    }

    companion object {
        val EMPTY = LogEventContext(null, null, null, null)

        fun of(screen: String?, owner: String?, flow: String?, step: String?): LogEventContext {
            return LogEventContext(normalized(screen), normalized(owner), normalized(flow), normalized(step))
        }

        private fun normalized(value: String?): String? = value?.takeIf { it.isNotBlank() && it != "unknown" }
    }
}

internal class ProducerMetadataBuffer {
    var elapsedUs: Long = 0L
        private set
    var threadId: Long = 0L
        private set
    var context: LogEventContext? = null
        private set

    fun set(elapsedUs: Long, threadId: Long, context: LogEventContext?) {
        this.elapsedUs = elapsedUs
        this.threadId = threadId
        this.context = context
    }

    fun capture(context: LogEventContext?): ProducerMetadataBuffer {
        set(
            elapsedUs = SystemClock.elapsedRealtimeNanos().coerceAtLeast(0L) / 1_000L,
            threadId = Process.myTid().toLong().coerceAtLeast(0L),
            context = context,
        )
        return this
    }
}

internal data class BinaryLogFileHeader(
    val runId: ByteArray,
    val processInstanceId: ByteArray,
    val sessionId: ByteArray,
    val segmentIndex: Long,
    val osPid: Long,
    val collectorStartElapsedUs: Long,
    val segmentStartElapsedUs: Long,
    val segmentStartUnixMs: Long,
    val identitySource: Long,
    val processName: String,
    val symbolNamespace: ByteArray,
    val processScope: Long = Jhlog.PROCESS_SCOPE_ALL,
    val allowedProcessCount: Long = 0L,
    val processScopeFingerprint: ByteArray = ByteArray(0),
    val previousSegmentDigest: ByteArray = ByteArray(0),
    val expectedProcessCount: Long = 1L,
    val expectedProcessFingerprint: ByteArray = ByteArray(32),
    val processRosterDeclarationComplete: Boolean = true,
    val requiredFeatures: Long = Jhlog.REQUIRED_FEATURES,
) {
    companion object {
        fun randomId(): ByteArray = ByteArray(16).also { bytes -> ThreadLocalRandom.current().nextBytes(bytes) }
    }
}
