package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicReference
import java.util.zip.GZIPInputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncLogWriterTest {
    @Test
    fun fileHeaderCarriesConfiguredStableSymbolNamespace() {
        val directory = Files.createTempDirectory("jankhunter-symbol-namespace").toFile()
        try {
            val namespace = ByteArray(16) { index -> index.toByte() }
            val writer = AsyncLogWriter.open(
                directory,
                JankHunterConfig.builder()
                    .symbolNamespace(namespace)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writer.counter("namespace.probe", 1L)
            assertTrue(writer.close())

            assertArrayEquals(namespace, fileSymbolNamespace(logFiles(directory).single()))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun criticalMetricPolicyCoversLifecycleSessionCrashAndHeapEvidence() {
        assertTrue(AsyncLogWriter.isCriticalMetricName("app.lifecycle.foreground.count"))
        assertTrue(AsyncLogWriter.isCriticalMetricName("screen.checkout.lifecycle.resumed.count"))
        assertTrue(AsyncLogWriter.isCriticalMetricName("jankhunter.runtime.session.start.count"))
        assertTrue(AsyncLogWriter.isCriticalMetricName("jankhunter.runtime.crash.count"))
        assertTrue(AsyncLogWriter.isCriticalMetricName("jankhunter.heap_dump.created.count"))
        assertFalse(AsyncLogWriter.isCriticalMetricName("runtime.method.calls"))
    }

    @Test
    fun openQualityAndFlushStayLazyUntilAnEventIsAccepted() {
        val root = Files.createTempDirectory("jankhunter-lazy-writer").toFile()
        val directory = File(root, "not-created")
        try {
            val writer = AsyncLogWriter.open(directory, config(), "main")

            writer.recordQuality(QualityCounterId.RUNTIME_STACK_MISMATCH)
            assertTrue(writer.flushBlocking())
            assertFalse(directory.exists())
            assertTrue(writer.close())
            assertFalse(directory.exists())
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun directBinaryWriterCloseUsesNormalReason() {
        val directory = Files.createTempDirectory("jankhunter-normal-close").toFile()
        try {
            val file = File(directory, "direct.jhlog")
            BinaryLogWriter(file).close()

            assertEquals(JhlogV9.SEGMENT_END_NORMAL, segmentEndReason(file))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun canonicalNameStartsAtZeroAndCloseUsesShutdownReason() {
        val directory = Files.createTempDirectory("jankhunter-session-name").toFile()
        try {
            val nowMs = 1_800_000_000_000L
            val expectedDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(nowMs))
            val writer = AsyncLogWriter.open(directory, config(), "private:process") { nowMs }

            writer.counter("first.session.counter", 1L)
            assertTrue(writer.close())

            val file = logFiles(directory).single()
            assertEquals("jh-session-log.$expectedDate.0.jhlog", file.name)
            assertEquals(JhlogV9.SEGMENT_END_SHUTDOWN, segmentEndReason(file))
            assertTrue(logFileText(file).contains("first.session.counter"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun sequentialSessionsUseSequentialIndices() {
        val directory = Files.createTempDirectory("jankhunter-session-sequence").toFile()
        try {
            val nowMs = 1_800_000_000_000L
            AsyncLogWriter.open(directory, config(), "main") { nowMs }.run {
                counter("first.session", 1L)
                close()
            }
            AsyncLogWriter.open(directory, config(), "main") { nowMs }.run {
                counter("second.session", 1L)
                close()
            }

            val indices = logFiles(directory)
                .mapNotNull { file -> SessionLogName.parse(file.name)?.index }
                .sorted()
            assertEquals(listOf(0L, 1L), indices)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun localDateIsFixedForTheLifetimeOfTheSession() {
        val directory = Files.createTempDirectory("jankhunter-session-date").toFile()
        try {
            var nowMs = 1_800_000_000_000L
            val expectedDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(nowMs))
            val writer = AsyncLogWriter.open(directory, config(), "main") { nowMs }
            nowMs += 3L * 24L * 60L * 60L * 1_000L

            repeat(32) { index -> writer.counter("after.midnight.$index", index.toLong()) }
            assertTrue(writer.close())

            assertEquals("jh-session-log.$expectedDate.0.jhlog", logFiles(directory).single().name)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun closeDrainsAcceptedQueueBeforeReturning() {
        val directory = Files.createTempDirectory("jankhunter-close-drain").toFile()
        try {
            val writer = AsyncLogWriter.open(
                directory,
                JankHunterConfig.builder().maxQueueSize(512).flushIntervalMs(60_000L).build(),
                "main",
            )
            repeat(128) { index -> writer.counter("close.queue.$index", index.toLong()) }

            assertTrue(writer.close())

            val text = logFileText(logFiles(directory).single())
            assertTrue(text.contains("close.queue.0"))
            assertTrue(text.contains("close.queue.127"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun enabledInternalLimitSealsOneFileWithoutRotation() {
        val directory = Files.createTempDirectory("jankhunter-size-limit").toFile()
        val limit = 1024L * 1024L
        try {
            val writer = AsyncLogWriter.open(
                directory,
                JankHunterConfig.builder()
                    .sessionLogSizeLimitEnabled(true)
                    .maxSessionLogSizeMiB(1)
                    .maxQueueSize(16_384)
                    .maxDictionaryEntries(16_384)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writeIncompressibleCounters(writer)

            assertTrue(writer.close(timeoutMs = 15_000L))

            val file = logFiles(directory).single()
            assertTrue("physical limit exceeded: ${file.length()} > $limit", file.length() <= limit)
            assertEquals(JhlogV9.SEGMENT_END_SIZE_LIMIT, segmentEndReason(file))
            assertTrue(
                (qualityCounters(file)[QualityCounterId.EVENT_LOST_AFTER_SIZE_LIMIT_TOTAL] ?: 0L) > 0L,
            )
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun disabledInternalLimitLeavesBuiltInStorageUnlimited() {
        val directory = Files.createTempDirectory("jankhunter-size-limit-disabled").toFile()
        val disabledLimit = 1024L * 1024L
        try {
            val writer = AsyncLogWriter.open(
                directory,
                JankHunterConfig.builder()
                    .sessionLogSizeLimitEnabled(false)
                    .maxSessionLogSizeMiB(1)
                    .maxQueueSize(16_384)
                    .maxDictionaryEntries(16_384)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writeIncompressibleCounters(writer)

            assertTrue(writer.close(timeoutMs = 15_000L))

            val file = logFiles(directory).single()
            assertTrue(
                "disabled internal limit still capped the file: ${file.length()} <= $disabledLimit",
                file.length() > disabledLimit,
            )
            assertEquals(JhlogV9.SEGMENT_END_SHUTDOWN, segmentEndReason(file))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun enabledInternalAndExternalLimitsUseTheSmallerValue() {
        val root = Files.createTempDirectory("jankhunter-external-limit").toFile()
        val storageLimit = 12L * 1024L
        try {
            val storage = TestBinaryStorage(File(root, "storage"), fileSizeLimitBytes = storageLimit)
            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .sessionLogSizeLimitEnabled(true)
                    .maxSessionLogSizeMiB(1)
                    .maxQueueSize(4_096)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            repeat(2_000) { index ->
                writer.counter("external.limit.counter.$index.${index.toString(16)}", index.toLong())
            }

            assertTrue(writer.close(timeoutMs = 5_000L))

            val file = storage.logFiles().single()
            assertTrue(file.length() <= storageLimit)
            assertEquals(JhlogV9.SEGMENT_END_SIZE_LIMIT, segmentEndReason(file))
            assertTrue(storage.cleanupProtectedPaths.flatten().any { it == file.absolutePath })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun disabledInternalLimitStillHonorsExternalStorageLimit() {
        val root = Files.createTempDirectory("jankhunter-disabled-internal-external-limit").toFile()
        val storageLimit = 12L * 1024L
        try {
            val storage = TestBinaryStorage(File(root, "storage"), fileSizeLimitBytes = storageLimit)
            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .sessionLogSizeLimitEnabled(false)
                    .maxSessionLogSizeMiB(1)
                    .maxQueueSize(4_096)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            repeat(2_000) { index ->
                writer.counter("external.only.limit.counter.$index.${index.toString(16)}", index.toLong())
            }

            assertTrue(writer.close(timeoutMs = 5_000L))

            val file = storage.logFiles().single()
            assertTrue(file.length() <= storageLimit)
            assertEquals(JhlogV9.SEGMENT_END_SIZE_LIMIT, segmentEndReason(file))
            assertTrue(storage.cleanupProtectedPaths.flatten().any { it == file.absolutePath })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun ioFailureDoesNotOpenReplacementOrReplayEvents() {
        val root = Files.createTempDirectory("jankhunter-io-failure").toFile()
        try {
            val storage = FailingBinaryStorage(File(root, "storage"), failAfterBytes = 1_024L)
            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .maxQueueSize(4_096)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            repeat(2_000) { index ->
                writer.counter("io.failure.counter.$index.${index.toString(16)}", index.toLong())
            }

            assertFalse(writer.flushBlocking(timeoutMs = 5_000L))
            assertTrue(writer.close(timeoutMs = 5_000L))

            assertEquals(1, storage.openedNames.size)
            assertEquals(1, storage.logFiles().size)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun unexpectedOpenFailureRejectsLaterEventsAndCloseDoesNotHang() {
        val root = Files.createTempDirectory("jankhunter-open-failure").toFile()
        try {
            val storage = BlockingThrowingOpenBinaryStorage()
            val terminalCallback = CountDownLatch(1)
            val terminalCallbackCount = AtomicInteger()
            val terminalReason = AtomicInteger()
            val terminalFailure = AtomicReference<Throwable?>()
            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder().binaryStorage(storage).build(),
                "main",
                onTerminalStop = { _, reason, failure ->
                    terminalCallbackCount.incrementAndGet()
                    terminalReason.set(reason)
                    terminalFailure.set(failure)
                    terminalCallback.countDown()
                },
            )

            writer.counter("accepted.before.failure", 1L)
            assertTrue(storage.awaitOpen())
            val flushSucceeded = AtomicBoolean(true)
            val flushThread = Thread {
                flushSucceeded.set(writer.flushBlocking(timeoutMs = 5_000L))
            }.also(Thread::start)
            assertTrue(awaitFlushWaitingForCompletion(flushThread))

            storage.failOpen()
            flushThread.join(5_000L)
            assertFalse(flushThread.isAlive)
            assertFalse(flushSucceeded.get())
            assertTrue(terminalCallback.await(5, TimeUnit.SECONDS))
            assertEquals(1, terminalCallbackCount.get())
            assertEquals(QualityCounterId.REASON_IO_LOST, terminalReason.get())
            assertTrue(terminalFailure.get() is IllegalStateException)
            writer.counter("rejected.after.failure", 1L)
            assertTrue(writer.close(timeoutMs = 5_000L))
            assertEquals(1, terminalCallbackCount.get())
            assertEquals(1, storage.openAttempts)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun nonEmptyCustomWriterIsANameCollisionAndUsesNextIndex() {
        val root = Files.createTempDirectory("jankhunter-writer-contract").toFile()
        try {
            val storageDirectory = File(root, "storage").apply { mkdirs() }
            val nowMs = 1_800_000_000_000L
            val date = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(nowMs))
            File(storageDirectory, SessionLogName.create(date, 0L)).writeBytes(byteArrayOf(1))
            val storage = TestBinaryStorage(storageDirectory)

            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder().binaryStorage(storage).build(),
                "main",
            ) { nowMs }
            writer.counter("after.collision", 1L)
            assertTrue(writer.close())

            assertEquals(
                listOf(SessionLogName.create(date, 1L)),
                storage.openedNames,
            )
            assertTrue(logFileText(File(storageDirectory, SessionLogName.create(date, 1L))).contains("after.collision"))
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun externalStorageStartsAtZeroAfterUnpublishedMetadataReservation() {
        val root = Files.createTempDirectory("jankhunter-external-zero-reset").toFile()
        try {
            val leaseDirectory = File(root, "leases")
            val storage = TestBinaryStorage(File(root, "storage"))
            val nowMs = 1_800_000_000_000L
            val date = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(nowMs))
            SessionLogAllocator.reserve(leaseDirectory, date).close()

            val writer = AsyncLogWriter.open(
                leaseDirectory,
                JankHunterConfig.builder().binaryStorage(storage).build(),
                "main",
            ) { nowMs }
            writer.counter("external.zero", 1L)
            assertTrue(writer.close())

            assertEquals(listOf(SessionLogName.create(date, 0L)), storage.openedNames)
            assertEquals(SessionLogName.create(date, 0L), storage.logFiles().single().name)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun invalidMetricsStayOutOfTheDataStreamAndReachQualitySnapshot() {
        val directory = Files.createTempDirectory("jankhunter-invalid-metric").toFile()
        try {
            val writer = AsyncLogWriter.open(directory, config(), "main")
            writer.counter("invalid.counter", -1L)
            writer.gauge("invalid.gauge", -1L)
            writer.counter("valid.counter", 1L)
            assertTrue(writer.close())

            val file = logFiles(directory).single()
            assertEquals(2L, qualityCounters(file)[QualityCounterId.INVALID_METRIC] ?: 0L)
            assertFalse(logFileText(file).contains("invalid.counter"))
            assertFalse(logFileText(file).contains("invalid.gauge"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun criticalEvidenceIsAdmittedAndMergedInOrderWhenBulkLaneIsFull() {
        val root = Files.createTempDirectory("jankhunter-critical-lane").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .maxQueueSize(1)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )

            writer.counter("bulk.first", 1L)
            assertTrue(storage.awaitOpen())
            writer.counter("bulk.rejected", 1L)
            writer.stall(
                screen = "Checkout",
                owner = "CheckoutOwner",
                flow = "checkout",
                step = "pay",
                stackHint = "critical.stall",
                durationMs = 900L,
                foreground = true,
            )
            writer.flush()
            storage.releaseOpen()
            assertTrue(storage.awaitWriterCreated())

            assertTrue(writer.close(timeoutMs = 5_000L))
            val file = storage.logFiles().single()
            val text = logFileText(file)
            assertTrue(text.contains("bulk.first"))
            assertFalse(text.contains("bulk.rejected"))
            assertTrue(text.contains("critical.stall"))
            assertTrue(text.indexOf("bulk.first") < text.indexOf("critical.stall"))
            assertEquals(1L, qualityCounters(file)[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun defaultCriticalReserveAbsorbsStartupFlowBurstWithoutBlockingProducer() {
        val root = Files.createTempDirectory("jankhunter-critical-flow-burst").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriter.open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .maxQueueSize(2048)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )

            writer.flowContext("Startup", "App", "launch", "0")
            assertTrue(storage.awaitOpen())
            repeat(199) { index ->
                writer.flowContext("Startup", "App", "launch", (index + 1).toString())
            }
            storage.releaseOpen()
            assertTrue(storage.awaitWriterCreated())

            assertTrue(writer.close(timeoutMs = 5_000L))
            val counters = qualityCounters(storage.logFiles().single())
            assertEquals(0L, counters[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun runtimeCallUsesStableZeroAndNegativeIdsWithoutOwnerDictionaryEntries() {
        val directory = Files.createTempDirectory("jankhunter-stable-runtime-call").toFile()
        try {
            val writer = AsyncLogWriter.open(directory, config(), "main")
            val batch = RuntimeCallBatch(1).apply {
                add("screen", 0L, "example.Caller.call", "flow", "step", -1L, "example.Callee.call", 3L, 12L, 7L)
            }
            writer.runtimeCalls(batch)
            assertTrue(writer.close())

            val file = logFiles(directory).single()
            val call = recordPayloads(file, JhlogV9.TYPE_RUNTIME_CALL).single()
            val caller = call.contextOwner
            val callee = readSymbolRef(call.bytes, call.offset)
            assertNotNull(caller)
            assertTrue(caller!!.stable)
            assertEquals(0L, caller.id)
            assertNotNull(callee)
            assertTrue(callee!!.stable)
            assertEquals(-1L, callee.id)

            val dictionaryKinds = recordPayloads(file, JhlogV9.TYPE_DICTIONARY).mapNotNull { payload ->
                readUvarint(payload.bytes, payload.offset)?.value
            }
            assertFalse("runtime caller/callee created DICT_OWNER", dictionaryKinds.contains(1L))
            assertTrue("runtime caller/callee did not create embedded stable definitions", dictionaryKinds.contains(14L))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun batchedQueueAccountingCountsLogicalWireRecords() {
        val directory = Files.createTempDirectory("jankhunter-batch-accounting").toFile()
        try {
            val writer = AsyncLogWriter.open(directory, config(), "main")
            writer.runtimeCalls(
                RuntimeCallBatch(2).apply {
                    add(null, 0L, null, null, 1L, 1L, 1L, 1L)
                    add(null, 1L, null, null, 2L, 1L, 1L, 1L)
                },
            )
            writer.stableCounters(
                StableCounterBatch(3).apply {
                    add(0L, 2L)
                    add(-1L, 3L)
                    add(2L, 4L)
                },
            )
            assertTrue(writer.close())

            val quality = qualityCounters(logFiles(directory).single())
            assertEquals(5L, quality[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
            assertEquals(5L, quality[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    private fun config(): JankHunterConfig {
        return JankHunterConfig.builder().flushIntervalMs(60_000L).build()
    }

    private fun writeIncompressibleCounters(writer: AsyncLogWriter) {
        var state = 0x6a09e667f3bcc909L
        repeat(12_000) { index ->
            val suffix = CharArray(220) {
                state = state xor (state shl 13)
                state = state xor (state ushr 7)
                state = state xor (state shl 17)
                (33 + (state and 0x5f).toInt()).toChar()
            }.concatToString()
            writer.counter("size.limit.$index.$suffix", index.toLong())
        }
    }

    private fun segmentEndReason(file: File): Long? {
        return recordPayloads(file, JhlogV9.TYPE_SEGMENT_END)
            .lastOrNull()
            ?.let { payload -> readUvarint(payload.bytes, payload.offset)?.value }
    }

    private fun qualityCounters(file: File): Map<Int, Long> {
        val latest = LinkedHashMap<Int, Long>()
        recordPayloads(file, JhlogV9.TYPE_QUALITY_SNAPSHOT).forEach { payload ->
            var cursor = payload.offset
            cursor = readUvarint(payload.bytes, cursor)?.nextOffset ?: return@forEach
            cursor = readUvarint(payload.bytes, cursor)?.nextOffset ?: return@forEach
            val count = readUvarint(payload.bytes, cursor) ?: return@forEach
            cursor = count.nextOffset
            repeat(count.value.coerceAtMost(1_024L).toInt()) {
                val id = readUvarint(payload.bytes, cursor) ?: return@forEach
                cursor = id.nextOffset
                val value = readUvarint(payload.bytes, cursor) ?: return@forEach
                cursor = value.nextOffset
                latest[id.value.toInt()] = value.value
            }
        }
        return latest
    }

    private fun recordPayloads(file: File, expectedType: Int): List<RecordPayload> {
        val payloads = ArrayList<RecordPayload>()
        committedRawChunks(file.readBytes()).forEach { raw ->
            var offset = 0
            while (offset < raw.size) {
                val length = readUvarint(raw, offset) ?: break
                val bodyStart = length.nextOffset
                val bodyEnd = bodyStart + length.value.toInt()
                if (bodyEnd < bodyStart || bodyEnd > raw.size) break
                var cursor = bodyStart
                val type = readUvarint(raw, cursor) ?: break
                cursor = type.nextOffset
                val flags = readUvarint(raw, cursor) ?: break
                cursor = flags.nextOffset
                if (flags.value and JhlogV9.ENVELOPE_HAS_TIME != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
                if (flags.value and JhlogV9.ENVELOPE_HAS_THREAD != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
                var contextOwner: SymbolWire? = null
                if (
                    flags.value and JhlogV9.ENVELOPE_HAS_CONTEXT != 0L &&
                    flags.value and JhlogV9.ENVELOPE_SAME_CONTEXT == 0L
                ) {
                    val presence = readUvarint(raw, cursor) ?: break
                    cursor = presence.nextOffset
                    repeat(4) { bit ->
                        if (presence.value and (1L shl bit) != 0L) {
                            val ref = readSymbolRef(raw, cursor) ?: return@forEach
                            if (bit == 1) contextOwner = ref
                            cursor = ref.nextOffset
                        }
                    }
                }
                if (flags.value and JhlogV9.ENVELOPE_HAS_ATTRIBUTES != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
                if (type.value == expectedType.toLong()) {
                    payloads += RecordPayload(raw, cursor, contextOwner)
                }
                offset = bodyEnd
            }
        }
        return payloads
    }

    private fun logFileText(file: File): String {
        val out = ByteArrayOutputStream()
        committedRawChunks(file.readBytes()).forEach(out::write)
        return String(out.toByteArray(), StandardCharsets.ISO_8859_1)
    }

    private fun committedRawChunks(fileBytes: ByteArray): List<ByteArray> {
        if (fileBytes.size < FILE_PREFIX_BYTES) return emptyList()
        val headerLength = readUInt32Le(fileBytes, MAGIC_SIZE).toInt()
        var offset = FILE_PREFIX_BYTES + headerLength
        if (offset < FILE_PREFIX_BYTES || offset > fileBytes.size) return emptyList()
        val chunks = ArrayList<ByteArray>()
        while (offset + JhlogV9.CHUNK_HEADER_BYTES <= fileBytes.size) {
            if (!fileBytes.matchesAscii(offset, "JHC9")) break
            val flags = readUInt16Le(fileBytes, offset + 6)
            val storedLength = readUInt32Le(fileBytes, offset + 12).toInt()
            val payloadStart = offset + JhlogV9.CHUNK_HEADER_BYTES
            val trailerStart = payloadStart + storedLength
            val chunkEnd = trailerStart + JhlogV9.COMMIT_TRAILER_BYTES
            if (storedLength < 0 || trailerStart < payloadStart || chunkEnd > fileBytes.size) break
            if (!fileBytes.matchesAscii(trailerStart, "JHCM")) break
            val stored = fileBytes.copyOfRange(payloadStart, trailerStart)
            chunks += if (flags and JhlogV9.CHUNK_FLAG_GZIP != 0) {
                GZIPInputStream(ByteArrayInputStream(stored)).use { input -> input.readBytes() }
            } else {
                stored
            }
            offset = chunkEnd
        }
        return chunks
    }

    private fun fileSymbolNamespace(file: File): ByteArray {
        val bytes = file.readBytes()
        val headerLength = readUInt32Le(bytes, MAGIC_SIZE).toInt()
        val headerEnd = FILE_PREFIX_BYTES + headerLength
        var cursor = FILE_PREFIX_BYTES
        repeat(3) { cursor = readUvarint(bytes, cursor)?.nextOffset ?: return ByteArray(0) }
        cursor += 16 * 3
        repeat(6) { cursor = readUvarint(bytes, cursor)?.nextOffset ?: return ByteArray(0) }
        val processNameLength = readUvarint(bytes, cursor) ?: return ByteArray(0)
        cursor = processNameLength.nextOffset + processNameLength.value.toInt()
        val namespaceLength = readUvarint(bytes, cursor) ?: return ByteArray(0)
        cursor = namespaceLength.nextOffset
        val end = cursor + namespaceLength.value.toInt()
        if (cursor < FILE_PREFIX_BYTES || end < cursor || end > headerEnd || end > bytes.size) return ByteArray(0)
        return bytes.copyOfRange(cursor, end)
    }

    private fun logFiles(directory: File): List<File> {
        return directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
            .orEmpty()
            .toList()
    }

    private fun readUvarint(bytes: ByteArray, start: Int): Varint? {
        var value = 0L
        var shift = 0
        var offset = start
        while (offset < bytes.size && shift < 64) {
            val byte = bytes[offset++].toInt() and 0xff
            value = value or ((byte and 0x7f).toLong() shl shift)
            if (byte and 0x80 == 0) return Varint(value, offset)
            shift += 7
        }
        return null
    }

    private fun readSymbolRef(bytes: ByteArray, start: Int): SymbolWire? {
        val token = readUvarint(bytes, start) ?: return null
        if (token.value == 0L) return SymbolWire(stable = false, id = 0L, nextOffset = token.nextOffset)
        if (token.value != 1L) {
            return SymbolWire(stable = false, id = token.value ushr 1, nextOffset = token.nextOffset)
        }
        val end = token.nextOffset + Long.SIZE_BYTES
        if (end > bytes.size) return null
        var id = 0L
        repeat(Long.SIZE_BYTES) { index ->
            id = id or ((bytes[token.nextOffset + index].toLong() and 0xffL) shl (index * Byte.SIZE_BITS))
        }
        return SymbolWire(stable = true, id = id, nextOffset = end)
    }

    private fun readUInt16Le(bytes: ByteArray, offset: Int): Int {
        return (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl 8)
    }

    private fun readUInt32Le(bytes: ByteArray, offset: Int): Long {
        var value = 0L
        repeat(Int.SIZE_BYTES) { index ->
            value = value or ((bytes[offset + index].toLong() and 0xffL) shl (index * 8))
        }
        return value
    }

    private fun ByteArray.matchesAscii(offset: Int, expected: String): Boolean {
        if (offset < 0 || offset + expected.length > size) return false
        return expected.indices.all { index -> this[offset + index].toInt() == expected[index].code }
    }

    private class TestBinaryStorage(
        val directory: File,
        override val fileSizeLimitBytes: Long = Long.MAX_VALUE,
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE,
    ) : JankHunterBinaryStorage {
        val openedNames = mutableListOf<String>()
        val cleanupProtectedPaths = mutableListOf<Set<String>>()

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            directory.mkdirs()
            openedNames += fileName
            return FileBinaryWriter(File(directory, fileName))
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact {
            directory.mkdirs()
            val file = File(directory, fileName)
            return object : JankHunterBinaryArtifact {
                override val path: String = file.absolutePath

                override fun commit() = cleanup(setOf(path))

                override fun abort() {
                    file.delete()
                }
            }
        }

        override fun cleanup(protectedPaths: Set<String>) {
            cleanupProtectedPaths += protectedPaths.toSet()
        }

        override fun listFiles(): List<String> = directory.listFiles().orEmpty().map { file -> file.absolutePath }

        fun logFiles(): List<File> {
            return directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
                .orEmpty()
                .toList()
        }
    }

    private class FailingBinaryStorage(
        private val directory: File,
        private val failAfterBytes: Long,
    ) : JankHunterBinaryStorage {
        val openedNames = mutableListOf<String>()

        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            directory.mkdirs()
            openedNames += fileName
            return FailingBinaryWriter(File(directory, fileName), failAfterBytes)
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact =
            error("Artifacts are not used in this test")

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = directory.listFiles().orEmpty().map { file -> file.absolutePath }

        fun logFiles(): List<File> {
            return directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
                .orEmpty()
                .toList()
        }
    }

    private class BlockingOpenBinaryStorage(
        private val directory: File,
    ) : JankHunterBinaryStorage {
        private val openStarted = CountDownLatch(1)
        private val allowOpen = CountDownLatch(1)
        private val writerCreated = CountDownLatch(1)

        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            openStarted.countDown()
            if (!allowOpen.await(5, TimeUnit.SECONDS)) throw IOException("timed out waiting to open test storage")
            directory.mkdirs()
            return FileBinaryWriter(File(directory, fileName)).also { writerCreated.countDown() }
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact =
            error("Artifacts are not used in this test")

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = directory.listFiles().orEmpty().map { file -> file.absolutePath }

        fun awaitOpen(): Boolean = openStarted.await(5, TimeUnit.SECONDS)

        fun releaseOpen() {
            allowOpen.countDown()
        }

        fun awaitWriterCreated(): Boolean = writerCreated.await(5, TimeUnit.SECONDS)

        fun logFiles(): List<File> {
            return directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
                .orEmpty()
                .toList()
        }
    }

    private fun awaitFlushWaitingForCompletion(flushThread: Thread): Boolean {
        val deadlineNs = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (System.nanoTime() < deadlineNs) {
            if (flushThread.state == Thread.State.WAITING || flushThread.state == Thread.State.TIMED_WAITING) {
                return true
            }
            Thread.yield()
        }
        return false
    }

    private class BlockingThrowingOpenBinaryStorage : JankHunterBinaryStorage {
        private val openStarted = CountDownLatch(1)
        private val allowFailure = CountDownLatch(1)

        var openAttempts = 0
            private set

        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            openAttempts++
            openStarted.countDown()
            if (!allowFailure.await(5, TimeUnit.SECONDS)) {
                throw IOException("timed out waiting to fail test storage")
            }
            throw IllegalStateException("injected unexpected open failure")
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact =
            error("Artifacts are not used in this test")

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = emptyList()

        fun awaitOpen(): Boolean = openStarted.await(5, TimeUnit.SECONDS)

        fun failOpen() {
            allowFailure.countDown()
        }
    }

    private open class FileBinaryWriter(private val file: File) : JankHunterBinaryWriter {
        private val output = FileOutputStream(file, true)
        protected var written = file.length()

        override val path: String = file.absolutePath

        override fun bytesWritten(): Long = written

        override fun writeByte(byte: Byte) {
            output.write(byte.toInt())
            written++
        }

        override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) {
            output.write(bytes, offset, length)
            written += length.toLong()
        }

        override fun flush() = output.flush()

        override fun close() = output.close()
    }

    private class FailingBinaryWriter(
        private val file: File,
        private val failAfterBytes: Long,
    ) : JankHunterBinaryWriter {
        private val output = FileOutputStream(file, true)
        private var written = file.length()

        override val path: String = file.absolutePath

        override fun bytesWritten(): Long = written

        override fun writeByte(byte: Byte) {
            writeBytes(byteArrayOf(byte), 0, 1)
        }

        override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) {
            val writable = (failAfterBytes - written).coerceIn(0L, length.toLong()).toInt()
            if (writable > 0) {
                output.write(bytes, offset, writable)
                written += writable.toLong()
            }
            if (writable != length) throw IOException("injected write failure")
        }

        override fun flush() = output.flush()

        override fun close() = output.close()
    }

    private data class Varint(val value: Long, val nextOffset: Int)

    private data class RecordPayload(
        val bytes: ByteArray,
        val offset: Int,
        val contextOwner: SymbolWire?,
    )

    private data class SymbolWire(
        val stable: Boolean,
        val id: Long,
        val nextOffset: Int,
    )

    private companion object {
        const val MAGIC_SIZE = 8
        const val FILE_PREFIX_BYTES = MAGIC_SIZE + Int.SIZE_BYTES * 2
    }
}
