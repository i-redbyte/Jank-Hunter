package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterContextSnapshot
import io.jankhunter.runtime.JankHunterHttpEvent
import io.jankhunter.runtime.JankHunterOperationAttributes
import io.jankhunter.runtime.JankHunterOperationKind
import io.jankhunter.runtime.JankHunterOperationOutcome
import io.jankhunter.runtime.JankHunterStorageSwitchResult
import io.jankhunter.runtime.RuntimeHookFailureTracker
import io.jankhunter.runtime.RuntimeHookFailureReason
import io.jankhunter.runtime.RuntimeHookEventTransport
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.security.MessageDigest
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
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class AsyncLogWriterTest {
    @Test
    fun failOpenHookFailuresAreSealedIntoQualityEvidence() {
        val directory = Files.createTempDirectory("jankhunter-hook-failure-quality").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")

            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.COLLECTOR)
            writer.counter("hook.failure.probe", 1L)
            assertTrue(writer.close())

            val quality = qualityCounters(sessionLogFiles(directory).single())
            assertEquals(
                1L,
                quality[QualityCounterId.RUNTIME_HOOK_FAILURE_TOTAL] ?: 0L,
            )
            assertEquals(1L, quality[QualityCounterId.RUNTIME_HOOK_COLLECTOR_FAILURE] ?: 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun hookFailuresFromPreviousSessionsDoNotDegradeNewSession() {
        val directory = Files.createTempDirectory("jankhunter-hook-failure-session-scope").toFile()
        try {
            RuntimeHookFailureTracker.record(RuntimeHookFailureReason.CONTEXT)
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")

            writer.counter("clean.session.probe", 1L)
            assertTrue(writer.close())

            val quality = qualityCounters(sessionLogFiles(directory).single())
            assertEquals(0L, quality[QualityCounterId.RUNTIME_HOOK_FAILURE_TOTAL] ?: 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun writerEmitsOnlyJhlogVersionThreeDotZeroDotZero() {
        val directory = Files.createTempDirectory("jankhunter-format-version").toFile()
        try {
            val file = File(directory, "format.jhlog")
            BinaryLogWriter(file).close()

            val prefix = file.readBytes().copyOf(Jhlog.FILE_MAGIC.size)
            assertArrayEquals(Jhlog.FILE_MAGIC, prefix)
            assertEquals(Jhlog.FORMAT_MARKER, prefix[7].toInt() and 0xff)
            assertEquals(Jhlog.FORMAT_MAJOR, prefix[8].toInt() and 0xff)
            assertEquals(Jhlog.FORMAT_MINOR, prefix[9].toInt() and 0xff)
            assertEquals(Jhlog.FORMAT_PATCH, prefix[10].toInt() and 0xff)
            assertEquals("3.0.0", Jhlog.FORMAT_VERSION)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesBoundedOperationLifecycle() {
        val directory = Files.createTempDirectory("jankhunter-v3-operation").toFile()
        try {
            val file = File(directory, "operation.jhlog")
            val attributes = JankHunterOperationAttributes.of(
                "source",
                "notification",
                "cache_state",
                "empty",
            )
            BinaryLogWriter(file).use { writer ->
                writer.operation(
                    name = "document.open",
                    operationId = 42L,
                    parentId = 41L,
                    phase = Jhlog.OPERATION_PHASE_FINISHED,
                    kind = JankHunterOperationKind.USER.wireValue,
                    outcome = JankHunterOperationOutcome.SUCCESS.wireValue,
                    durationUs = 1_250_000L,
                    budgetUs = 2_000_000L,
                    attributes = attributes,
                )
            }

            val operation = recordPayloads(file, Jhlog.TYPE_OPERATION).single()
            val name = requireNotNull(readSymbolRef(operation.bytes, operation.offset))
            assertFalse(name.stable)
            val values = readUvarintValues(operation.copy(offset = name.nextOffset), 8)
            assertEquals(
                listOf(42L, 41L, Jhlog.OPERATION_PHASE_FINISHED, 1L, 1L, 1_250_000L, 2_000_000L, 2L),
                values,
            )
            assertTrue(logFileText(file).contains("document.open"))
            assertTrue(logFileText(file).contains("notification"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun asyncOperationKeepsStartContextWhenProducerContextChanges() {
        val directory = Files.createTempDirectory("jankhunter-operation-start-context").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            writer.updateProducerContext("StartScreen", "StartOwner")
            assertTrue(
                writer.operation(
                    name = "document.open",
                    operationId = 42L,
                    parentId = 0L,
                    phase = Jhlog.OPERATION_PHASE_STARTED,
                    kind = JankHunterOperationKind.SCREEN.wireValue,
                    outcome = 0L,
                    durationUs = 0L,
                    budgetUs = 1_000_000L,
                    screen = "StartScreen",
                    owner = "StartOwner",
                    attributes = JankHunterOperationAttributes.EMPTY,
                ),
            )
            writer.updateProducerContext("FinishScreen", "FinishOwner")
            assertTrue(
                writer.operation(
                    name = "document.open",
                    operationId = 42L,
                    parentId = 0L,
                    phase = Jhlog.OPERATION_PHASE_FINISHED,
                    kind = JankHunterOperationKind.SCREEN.wireValue,
                    outcome = JankHunterOperationOutcome.SUCCESS.wireValue,
                    durationUs = 500_000L,
                    budgetUs = 1_000_000L,
                    screen = "StartScreen",
                    owner = "StartOwner",
                    attributes = JankHunterOperationAttributes.EMPTY,
                ),
            )
            assertTrue(writer.close())

            val operations = recordPayloads(sessionLogFiles(directory).single(), Jhlog.TYPE_OPERATION)
            assertEquals(2, operations.size)
            assertNotNull(operations[0].contextScreen)
            assertNotNull(operations[0].contextOwner)
            assertEquals(operations[0].contextScreen?.id, operations[1].contextScreen?.id)
            assertEquals(operations[0].contextOwner?.id, operations[1].contextOwner?.id)
            assertEquals(42L, operations[0].contextOperationId)
            assertEquals(42L, operations[1].contextOperationId)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun aggregatedLogSpamKeepsProducerOperationId() {
        val directory = Files.createTempDirectory("jankhunter-log-spam-operation").toFile()
        val writer = AsyncLogWriterFactory().open(directory, config(), "main")
        val transport = RuntimeHookEventTransport({ 16 }, { 16 })
        transport.start(writer)
        try {
            assertTrue(
                transport.recordLogSpam(
                    screen = "Catalog",
                    owner = "CatalogPresenter",
                    source = "android.util.Log.d",
                    level = 3,
                    operationId = 42L,
                ),
            )
            assertTrue(transport.stopAndFlush(5_000L))
            assertTrue(writer.close())

            val record = recordPayloads(sessionLogFiles(directory).single(), Jhlog.TYPE_LOG_SPAM).single()
            assertEquals(42L, record.contextOperationId)
        } finally {
            transport.stopAndFlush(5_000L)
            transport.clear()
            writer.close()
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesTypedCollectorUiExitAndIoEvidence() {
        val directory = Files.createTempDirectory("jankhunter-v3-typed-evidence").toFile()
        try {
            val file = File(directory, "typed-evidence.jhlog")
            val buckets = LongArray(Jhlog.UI_FRAME_HISTOGRAM_BUCKET_COUNT).also {
                it[1] = 50L
                it[5] = 45L
                it[8] = 5L
            }
            BinaryLogWriter(file).use { writer ->
                writer.session(
                    null,
                    null,
                    null,
                    35,
                    null,
                    null,
                    null,
                    null,
                    null,
                    null,
                    null,
                    null,
                    null,
                    false,
                    Jhlog.COLLECTOR_FPS or Jhlog.COLLECTOR_JANKSTATS or
                        Jhlog.COLLECTOR_PROCESS_EXIT or Jhlog.COLLECTOR_IO_TRACING,
                )
                writer.uiWindow(
                    screen = null,
                    windowMs = 10_000L,
                    frameCount = 100L,
                    jankCount = 5L,
                    source = Jhlog.UI_SOURCE_JANKSTATS,
                    frameDeadlineUs = 16_667L,
                    frameDurationBuckets = buckets,
                )
                writer.processExit(6L, 1_750_000_000_000L, 100L, 256_000L, 320_000L, null)
                writer.io(
                    operation = Jhlog.IO_FILE_READ,
                    durationUs = 275_000L,
                    bytes = 4_096L,
                    mainThread = true,
                    sourceId = 0x32621L,
                    sourceName = "com.app.Storage.read",
                    outcome = Jhlog.IO_OUTCOME_FAILURE,
                    bytesKnown = true,
                )
            }

            val session = recordPayloads(file, Jhlog.TYPE_SESSION).single()
            var cursor = session.offset
            repeat(3) { cursor = requireNotNull(readSymbolRef(session.bytes, cursor)).nextOffset }
            cursor = requireNotNull(readUvarint(session.bytes, cursor)).nextOffset
            repeat(9) { cursor = requireNotNull(readSymbolRef(session.bytes, cursor)).nextOffset }
            val collectorFlags = requireNotNull(readUvarint(session.bytes, cursor)).value
            assertEquals(
                Jhlog.COLLECTOR_FPS or Jhlog.COLLECTOR_JANKSTATS or
                    Jhlog.COLLECTOR_PROCESS_EXIT or Jhlog.COLLECTOR_IO_TRACING,
                collectorFlags,
            )

            val ui = readUvarintValues(recordPayloads(file, Jhlog.TYPE_UI_WINDOW).single(), 18)
            assertEquals(listOf(10_000L, 100L, 5L, Jhlog.UI_SOURCE_JANKSTATS, 16_667L), ui.take(5))
            assertEquals(buckets.toList(), ui.drop(5))
            assertEquals(
                listOf(6L, 1_750_000_000_000L, 100L, 256_000L, 320_000L),
                readUvarintValues(recordPayloads(file, Jhlog.TYPE_PROCESS_EXIT).single(), 5),
            )
            val io = recordPayloads(file, Jhlog.TYPE_IO).single()
            val source = requireNotNull(readSymbolRef(io.bytes, io.offset))
            assertEquals(0x32621L, source.id)
            assertEquals(true, source.stable)
            assertEquals(
                listOf(Jhlog.IO_FILE_READ, Jhlog.IO_OUTCOME_FAILURE, 275_000L, 4_096L),
                readUvarintValues(io.copy(offset = source.nextOffset), 4),
            )
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesTypedWorkerLifecycleWithStableIdentity() {
        val directory = Files.createTempDirectory("jankhunter-v3-worker-lifecycle").toFile()
        try {
            val file = File(directory, "worker-lifecycle.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.worker(
                    workerId = 0L,
                    workerName = null,
                    instanceId = 0x32621L,
                    stage = Jhlog.WORKER_STAGE_ENQUEUED,
                    outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
                    durationMs = 0L,
                    runAttempt = 0L,
                    generation = 0L,
                    stopReason = 0L,
                    flags = Jhlog.FLAG_WORKER_PERIODIC,
                )
                writer.worker(
                    workerId = 0x1234L,
                    workerName = "com.app.SyncWorker.doWork",
                    instanceId = 0x32621L,
                    stage = Jhlog.WORKER_STAGE_STARTED,
                    outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
                    durationMs = 0L,
                    runAttempt = 2L,
                    generation = 3L,
                    stopReason = 0L,
                    flags = 0L,
                )
                writer.worker(
                    workerId = 0x1234L,
                    workerName = "com.app.SyncWorker.doWork",
                    instanceId = 0x32621L,
                    stage = Jhlog.WORKER_STAGE_FINISHED,
                    outcome = Jhlog.WORKER_OUTCOME_CANCELLED,
                    durationMs = 500L,
                    runAttempt = 2L,
                    generation = 3L,
                    stopReason = 4L,
                    flags = Jhlog.FLAG_WORKER_STOP_REASON_KNOWN,
                )
            }

            val payloads = recordPayloads(file, Jhlog.TYPE_WORKER)
            assertEquals(3, payloads.size)
            val enqueuedRef = requireNotNull(readSymbolRef(payloads[0].bytes, payloads[0].offset))
            assertFalse(enqueuedRef.stable)
            assertEquals(0L, enqueuedRef.id)
            assertEquals(
                listOf(0x32621L, Jhlog.WORKER_STAGE_ENQUEUED, 0L, 0L, 0L, 0L, 0L),
                readUvarintValues(RecordPayload(payloads[0].bytes, enqueuedRef.nextOffset, null), 7),
            )
            val startedRef = requireNotNull(readSymbolRef(payloads[1].bytes, payloads[1].offset))
            assertTrue(startedRef.stable)
            assertEquals(0x1234L, startedRef.id)
            assertEquals(
                listOf(0x32621L, Jhlog.WORKER_STAGE_STARTED, 0L, 0L, 2L, 3L, 0L),
                readUvarintValues(RecordPayload(payloads[1].bytes, startedRef.nextOffset, null), 7),
            )
            val finishedRef = requireNotNull(readSymbolRef(payloads[2].bytes, payloads[2].offset))
            assertEquals(
                listOf(0x32621L, Jhlog.WORKER_STAGE_FINISHED, Jhlog.WORKER_OUTCOME_CANCELLED, 500L, 2L, 3L, 4L),
                readUvarintValues(RecordPayload(payloads[2].bytes, finishedRef.nextOffset, null), 7),
            )
            assertTrue(logFileText(file).contains("com.app.SyncWorker.doWork"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesAdvancedHttpEvidenceWithoutRawEndpointData() {
        val directory = Files.createTempDirectory("jankhunter-v3-advanced-http").toFile()
        try {
            val file = File(directory, "advanced-http.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.http(
                    owner = "FeedRepository",
                    route = "GET /messages/{id}",
                    event = JankHunterHttpEvent(
                        JankHunterContextSnapshot(
                            screen = null,
                            owner = "FeedRepository",
                            initiatorPresent = true,
                            initiatorId = 0L,
                            initiatorName = "FeedRepository.loadMessages",
                        ),
                        "GET /messages/{id}",
                        "mail-api",
                        850L,
                        30L,
                        12L,
                        45L,
                        20L,
                        8L,
                        510L,
                        220L,
                        503,
                        Jhlog.HTTP_FAILURE_PHASE_RESPONSE.toInt(),
                        Jhlog.HTTP_FAILURE_KIND_PROTOCOL.toInt(),
                        Jhlog.HTTP_PROTOCOL_2.toInt(),
                        42_120L,
                        740L,
                        2,
                        1,
                        2,
                        1,
                        1,
                        0,
                        1,
                        0L,
                    ),
                    flags = Jhlog.FLAG_HTTP_FAILED or Jhlog.FLAG_HTTP_REQUEST_BYTES_KNOWN or
                        Jhlog.FLAG_HTTP_RESPONSE_BYTES_KNOWN,
                )
            }

            val payload = recordPayloads(file, Jhlog.TYPE_HTTP).single()
            val route = requireNotNull(readSymbolRef(payload.bytes, payload.offset))
            val service = requireNotNull(readSymbolRef(payload.bytes, route.nextOffset))
            val initiator = requireNotNull(readSymbolRef(payload.bytes, service.nextOffset))
            assertFalse(route.stable)
            assertFalse(service.stable)
            assertTrue(initiator.stable)
            assertEquals(0L, initiator.id)
            assertEquals(
                listOf(
                    850L, 30L, 12L, 45L, 20L, 8L, 510L, 220L,
                    503L, Jhlog.HTTP_FAILURE_PHASE_RESPONSE, Jhlog.HTTP_FAILURE_KIND_PROTOCOL,
                    Jhlog.HTTP_PROTOCOL_2, 42_120L, 740L, 2L, 1L, 2L, 1L, 1L, 0L, 1L,
                ),
                readUvarintValues(RecordPayload(payload.bytes, initiator.nextOffset, null), 21),
            )
            val dictionaryText = logFileText(file)
            assertTrue(dictionaryText.contains("GET /messages/{id}"))
            assertTrue(dictionaryText.contains("mail-api"))
            assertTrue(dictionaryText.contains("FeedRepository.loadMessages"))
            assertFalse(dictionaryText.contains("https://"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesBoundedWebSocketLifecycleEvidence() {
        val directory = Files.createTempDirectory("jankhunter-v3-websocket").toFile()
        try {
            val file = File(directory, "websocket.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.webSocket(
                    owner = "RealtimeRepository",
                    event = io.jankhunter.runtime.JankHunterWebSocketEvent(
                        null,
                        "GET /socket/{channel}",
                        "RealtimeRepository",
                        0x32621L,
                        io.jankhunter.runtime.JankHunterWebSocketEvent.STAGE_CLOSED,
                        12_000L,
                        101,
                        1000,
                        io.jankhunter.runtime.JankHunterWebSocketEvent.FAILURE_UNKNOWN,
                        7L,
                        3L,
                        4_096L,
                        1,
                    ),
                )
            }

            val payload = recordPayloads(file, Jhlog.TYPE_WEBSOCKET).single()
            val route = requireNotNull(readSymbolRef(payload.bytes, payload.offset))
            assertFalse(route.stable)
            assertEquals(
                listOf(
                    0x32621L,
                    Jhlog.WEBSOCKET_STAGE_CLOSED,
                    12_000L,
                    101L,
                    1000L,
                    Jhlog.WEBSOCKET_FAILURE_UNKNOWN,
                    7L,
                    3L,
                    4_096L,
                    1L,
                ),
                readUvarintValues(RecordPayload(payload.bytes, route.nextOffset, null), 10),
            )
            val text = logFileText(file)
            assertTrue(text.contains("GET /socket/{channel}"))
            assertFalse(text.contains("wss://"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesTypedDatabaseCallWithoutBoundValues() {
        val directory = Files.createTempDirectory("jankhunter-v3-database").toFile()
        try {
            val file = File(directory, "database.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.database(
                    sourceId = 0x32621L,
                    sourceName = "com.app.MessagesDao.load",
                    query = "SELECT * FROM messages WHERE id = ?",
                    framework = Jhlog.DATABASE_FRAMEWORK_ROOM,
                    operation = Jhlog.DATABASE_OPERATION_QUERY,
                    outcome = Jhlog.DATABASE_OUTCOME_SUCCESS,
                    failureKind = Jhlog.DATABASE_FAILURE_NONE,
                    boundary = Jhlog.DATABASE_BOUNDARY_EXECUTE,
                    statementFingerprint = 0x7123L,
                    resultKnown = true,
                    resultKind = Jhlog.DATABASE_RESULT_AFFECTED_ROWS,
                    resultCountBucket = Jhlog.DATABASE_COUNT_TWO_TO_TEN,
                    transactionId = 17L,
                    statementToken = 29L,
                    phaseMask = Jhlog.DATABASE_PHASE_LOCK_WAIT or Jhlog.DATABASE_PHASE_EXECUTE,
                    poolWaitUs = 0L,
                    lockWaitUs = 10_000L,
                    executeUs = 200_000L,
                    materializeUs = 0L,
                    durationUs = 250_000L,
                    mainThread = true,
                )
            }

            val payload = recordPayloads(file, Jhlog.TYPE_DATABASE).single()
            val query = requireNotNull(readSymbolRef(payload.bytes, payload.offset))
            val source = requireNotNull(readSymbolRef(payload.bytes, query.nextOffset))
            assertFalse(query.stable)
            assertTrue(source.stable)
            assertEquals(
                listOf(
                    0x7123L,
                    Jhlog.DATABASE_FRAMEWORK_ROOM,
                    Jhlog.DATABASE_OPERATION_QUERY,
                    Jhlog.DATABASE_OUTCOME_SUCCESS,
                    Jhlog.DATABASE_FAILURE_NONE,
                    Jhlog.DATABASE_BOUNDARY_EXECUTE,
                    1L,
                    Jhlog.DATABASE_RESULT_AFFECTED_ROWS,
                    Jhlog.DATABASE_COUNT_TWO_TO_TEN,
                    17L,
                    29L,
                    Jhlog.DATABASE_PHASE_LOCK_WAIT or Jhlog.DATABASE_PHASE_EXECUTE,
                    0L,
                    10_000L,
                    200_000L,
                    0L,
                    250_000L,
                ),
                readUvarintValues(RecordPayload(payload.bytes, source.nextOffset, null), 17),
            )
            val text = logFileText(file)
            assertTrue(text.contains("SELECT * FROM messages WHERE id = ?"))
            assertFalse(text.contains("secret-value"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesDatabaseTransactionLifecycle() {
        val directory = Files.createTempDirectory("jankhunter-v3-database-transaction").toFile()
        try {
            val file = File(directory, "database-transaction.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.databaseTransaction(
                    sourceId = 0x32621L,
                    sourceName = "com.app.MessagesDao.save",
                    transactionId = 7L,
                    parentId = 3L,
                    stage = Jhlog.DATABASE_TRANSACTION_TERMINAL,
                    mode = Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
                    outcome = Jhlog.DATABASE_TRANSACTION_SUCCESS,
                    failureKind = Jhlog.DATABASE_FAILURE_NONE,
                    durationUs = 100_000L,
                    statementCount = 4L,
                    readCount = 3L,
                    writeCount = 1L,
                    mainThread = true,
                )
            }

            val payload = recordPayloads(file, Jhlog.TYPE_DATABASE_TRANSACTION).single()
            val source = requireNotNull(readSymbolRef(payload.bytes, payload.offset))
            assertTrue(source.stable)
            assertEquals(
                listOf(
                    7L,
                    3L,
                    Jhlog.DATABASE_TRANSACTION_TERMINAL,
                    Jhlog.DATABASE_TRANSACTION_IMMEDIATE,
                    Jhlog.DATABASE_TRANSACTION_SUCCESS,
                    Jhlog.DATABASE_FAILURE_NONE,
                    100_000L,
                    4L,
                    3L,
                    1L,
                ),
                readUvarintValues(RecordPayload(payload.bytes, source.nextOffset, null), 10),
            )
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun versionThreeWritesTypedAndroidComponentAndIpcRecords() {
        val directory = Files.createTempDirectory("jankhunter-v3-android-components").toFile()
        try {
            val file = File(directory, "android-components.jhlog")
            BinaryLogWriter(file).use { writer ->
                writer.processState(
                    uiVisibility = Jhlog.PROCESS_UI_VISIBLE,
                    processImportance = Jhlog.PROCESS_IMPORTANCE_FOREGROUND_SERVICE,
                    androidImportance = 125L,
                    reason = Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE,
                )
                writer.androidComponent(
                    componentId = 0x419L,
                    componentName = "com.app.SyncService",
                    action = "com.app.action.SYNC",
                    instanceId = 11L,
                    flowId = 12L,
                    kind = Jhlog.COMPONENT_KIND_SERVICE,
                    stage = Jhlog.COMPONENT_SERVICE_START_COMMAND,
                    outcome = Jhlog.COMPONENT_OUTCOME_SUCCESS,
                    durationUs = 9_000L,
                    componentFlags = Jhlog.COMPONENT_FLAG_FOREGROUND,
                )
                writer.binderTransaction(
                    descriptor = "com.app.sync.ISyncService",
                    method = "syncNow",
                    callId = 31L,
                    direction = Jhlog.BINDER_DIRECTION_CLIENT,
                    transactionCode = 7L,
                    outcome = Jhlog.BINDER_OUTCOME_SUCCESS,
                    failureKind = Jhlog.BINDER_FAILURE_NONE,
                    durationUs = 4_000L,
                    binderFlags = Jhlog.BINDER_FLAG_ONEWAY,
                    mainThread = true,
                )
            }

            assertEquals(
                listOf(
                    Jhlog.PROCESS_UI_VISIBLE,
                    Jhlog.PROCESS_IMPORTANCE_FOREGROUND_SERVICE,
                    125L,
                    Jhlog.PROCESS_STATE_COMPONENT_LIFECYCLE,
                ),
                readUvarintValues(recordPayloads(file, Jhlog.TYPE_PROCESS_STATE).single(), 4),
            )

            val componentPayload = recordPayloads(file, Jhlog.TYPE_ANDROID_COMPONENT).single()
            val component = requireNotNull(readSymbolRef(componentPayload.bytes, componentPayload.offset))
            val action = requireNotNull(readSymbolRef(componentPayload.bytes, component.nextOffset))
            assertTrue(component.stable)
            assertFalse(action.stable)
            assertEquals(
                listOf(
                    11L,
                    12L,
                    Jhlog.COMPONENT_KIND_SERVICE,
                    Jhlog.COMPONENT_SERVICE_START_COMMAND,
                    Jhlog.COMPONENT_OUTCOME_SUCCESS,
                    9_000L,
                    Jhlog.COMPONENT_FLAG_FOREGROUND,
                ),
                readUvarintValues(RecordPayload(componentPayload.bytes, action.nextOffset, null), 7),
            )

            val binderPayload = recordPayloads(file, Jhlog.TYPE_BINDER_TRANSACTION).single()
            val descriptor = requireNotNull(readSymbolRef(binderPayload.bytes, binderPayload.offset))
            val method = requireNotNull(readSymbolRef(binderPayload.bytes, descriptor.nextOffset))
            assertFalse(descriptor.stable)
            assertFalse(method.stable)
            assertEquals(
                listOf(
                    31L,
                    Jhlog.BINDER_DIRECTION_CLIENT,
                    7L,
                    Jhlog.BINDER_OUTCOME_SUCCESS,
                    Jhlog.BINDER_FAILURE_NONE,
                    4_000L,
                    Jhlog.BINDER_FLAG_ONEWAY,
                ),
                readUvarintValues(RecordPayload(binderPayload.bytes, method.nextOffset, null), 7),
            )
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun fileHeaderDeclaresExactExpectedProcessRoster() {
        val directory = Files.createTempDirectory("jankhunter-process-roster").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(
                directory = directory,
                config = config(),
                processName = "main",
                expectedProcesses = setOf("remote", "main"),
                rosterDeclarationComplete = true,
                onTerminalStop = { _, _, _ -> },
            )
            writer.counter("roster", 1L)
            assertTrue(writer.close())

            val header = fileSegmentHeader(sessionLogFiles(directory).single())
            assertEquals(2L, header.expectedProcessCount)
            assertTrue(header.rosterDeclarationComplete)
            assertArrayEquals(processFingerprint(setOf("main", "remote")), header.expectedProcessFingerprint)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun concurrentProcessWritersPersistOneRunCohortIdentity() {
        val directory = Files.createTempDirectory("jankhunter-process-cohort").toFile()
        try {
            val expectedProcesses = setOf("main", "remote")
            val main = AsyncLogWriterFactory().open(
                directory = directory,
                config = config(),
                processName = "main",
                expectedProcesses = expectedProcesses,
                rosterDeclarationComplete = true,
                onTerminalStop = { _, _, _ -> },
            )
            val remote = AsyncLogWriterFactory().open(
                directory = directory,
                config = config(),
                processName = "remote",
                expectedProcesses = expectedProcesses,
                rosterDeclarationComplete = true,
                onTerminalStop = { _, _, _ -> },
            )

            main.counter("main", 1L)
            remote.counter("remote", 1L)
            assertTrue(main.flushBlocking())
            assertTrue(remote.flushBlocking())
            assertTrue(main.close())
            assertTrue(remote.close())

            val headers = sessionLogFiles(directory).map(::fileSegmentHeader)
            assertEquals(2, headers.size)
            assertArrayEquals(headers.first().runId, headers.last().runId)
            assertEquals(2, growthHistoryFiles(directory).size)
            assertEquals(1, requireNotNull(main.logGrowthSummary()).recentSessions.size)
            assertEquals(1, requireNotNull(remote.logGrowthSummary()).recentSessions.size)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun fileHeaderCarriesConfiguredStableSymbolNamespace() {
        val directory = Files.createTempDirectory("jankhunter-symbol-namespace").toFile()
        try {
            val namespace = ByteArray(16) { index -> index.toByte() }
            val writer = AsyncLogWriterFactory().open(
                directory,
                JankHunterConfig.builder()
                    .symbolNamespace(namespace)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writer.counter("namespace.probe", 1L)
            assertTrue(writer.close())

            assertArrayEquals(namespace, fileSymbolNamespace(sessionLogFiles(directory).single()))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun fileHeaderCarriesConfiguredProcessScopeWithoutChangingFormatVersion() {
        val root = Files.createTempDirectory("jankhunter-process-scope").toFile()
        try {
            val cases = listOf(
                Triple("all", JankHunterConfig.builder(), ProcessScopeWire(Jhlog.PROCESS_SCOPE_ALL, 0L, ByteArray(0))),
                Triple(
                    "main",
                    JankHunterConfig.builder().mainProcessOnly(true),
                    ProcessScopeWire(Jhlog.PROCESS_SCOPE_MAIN_ONLY, 0L, ByteArray(0)),
                ),
                Triple(
                    "allowlist",
                    JankHunterConfig.builder().allowedProcesses(listOf("com.example", "com.example:sync")),
                    ProcessScopeWire(
                        Jhlog.PROCESS_SCOPE_ALLOWLIST,
                        2L,
                        byteArrayOf(
                            0xd7.toByte(), 0xde.toByte(), 0x8e.toByte(), 0x71,
                            0x64, 0x14, 0xeb.toByte(), 0x05, 0xdf.toByte(), 0xdf.toByte(),
                            0xde.toByte(), 0xc8.toByte(), 0x72, 0x1a, 0xa6.toByte(), 0x1f,
                            0x80.toByte(), 0xa9.toByte(), 0xfe.toByte(), 0x2e, 0x80.toByte(), 0x39,
                            0x79, 0x55, 0xa3.toByte(), 0xac.toByte(), 0xd8.toByte(), 0x96.toByte(),
                            0xaa.toByte(), 0x2c, 0x7a, 0x48,
                        ),
                    ),
                ),
            )
            cases.forEach { (name, builder, expected) ->
                val directory = File(root, name)
                val writer = AsyncLogWriterFactory().open(
                    directory,
                    builder.flushIntervalMs(60_000L).build(),
                    "com.example",
                )
                writer.counter("scope.$name", 1L)
                assertTrue(writer.close())

                val file = sessionLogFiles(directory).single()
                assertEquals(expected, fileProcessScope(file))
                assertTrue(fileSegmentHeader(file).requiredFeatures and Jhlog.FEATURE_PROCESS_SCOPE != 0L)
                assertTrue(fileSegmentHeader(file).requiredFeatures and Jhlog.FEATURE_COLUMNAR_RUNTIME_CALLS != 0L)
                assertTrue(fileSegmentHeader(file).requiredFeatures and Jhlog.FEATURE_SEGMENT_DIGEST_CHAIN != 0L)
                assertArrayEquals(Jhlog.FILE_MAGIC, file.readBytes().copyOf(Jhlog.FILE_MAGIC.size))
            }
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun criticalMetricPolicyCoversLifecycleSessionCrashAndHeapEvidence() {
        assertTrue(AsyncLogWriter.isCriticalMetricName("app.lifecycle.ui_visible.count"))
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
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")

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
    fun explicitGrowthWriteCreatesSnapshotBeforeAnyEventIsAccepted() {
        val root = Files.createTempDirectory("jankhunter-empty-growth-write").toFile()
        val directory = File(root, "created-on-demand")
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")

            assertTrue(writer.writeLogGrowthSummaryBlocking())

            assertEquals(1, growthHistoryFiles(directory).size)
            assertNotNull(writer.logGrowthSummary()?.currentSession)
            assertTrue(writer.close())

            val file = sessionLogFiles(directory).single()
            val kinds = recordPayloads(file, Jhlog.TYPE_LOG_GROWTH).mapNotNull { payload ->
                readUvarint(payload.bytes, payload.offset)?.value
            }
            assertTrue(kinds.contains(Jhlog.LOG_GROWTH_HISTORY))
            assertTrue(kinds.count { it == Jhlog.LOG_GROWTH_LIVE } >= 3)
            assertEquals(
                committedRawChunks(file.readBytes()).size.toLong(),
                qualityCounters(file)[QualityCounterId.COMMITTED_CHUNK_TOTAL],
            )
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

            assertEquals(Jhlog.SEGMENT_END_NORMAL, segmentEndReason(file))
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
            val writer = AsyncLogWriterFactory { nowMs }.open(directory, config(), "private:process")

            writer.counter("first.session.counter", 1L)
            assertTrue(writer.close())

            val file = sessionLogFiles(directory).single()
            val parsed = requireNotNull(SessionLogName.parse(file.name))
            assertEquals(expectedDate, parsed.localDate)
            assertEquals(32, requireNotNull(parsed.runId).length)
            assertEquals(0L, parsed.dailySessionIndex)
            assertEquals(0L, parsed.segmentIndex)
            assertEquals(Jhlog.SEGMENT_END_SHUTDOWN, segmentEndReason(file))
            assertTrue(logFileText(file).contains("first.session.counter"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun sequentialSessionsUseSequentialIndices() {
        val directory = Files.createTempDirectory("jankhunter-session-sequence").toFile()
        try {
            var nowMs = 1_800_000_000_000L
            val firstDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(nowMs))
            AsyncLogWriterFactory { nowMs }.open(directory, config(), "main").run {
                counter("first.session", 1L)
                close()
            }
            AsyncLogWriterFactory { nowMs }.open(directory, config(), "main").run {
                counter("second.session", 1L)
                close()
            }
            nowMs += 24L * 60L * 60L * 1_000L
            val secondDate = SimpleDateFormat("yyyy-MM-dd", Locale.US).format(Date(nowMs))
            AsyncLogWriterFactory { nowMs }.open(directory, config(), "main").run {
                counter("next.day.session", 1L)
                close()
            }

            val parsed = sessionLogFiles(directory).mapNotNull { file -> SessionLogName.parse(file.name) }
            assertEquals(listOf(0L, 1L), parsed.filter { it.localDate == firstDate }.map { it.dailySessionIndex }.sorted())
            assertEquals(listOf(0L), parsed.filter { it.localDate == secondDate }.map { it.dailySessionIndex })
            assertTrue(parsed.all { it.segmentIndex == 0L })
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun concurrentProcessesShareDailySessionAndUseDistinctSegments() {
        val directory = Files.createTempDirectory("jankhunter-session-multiprocess-name").toFile()
        try {
            val nowMs = 1_800_000_000_000L
            val main = AsyncLogWriterFactory { nowMs }.open(directory, config(), "main")
            val remote = AsyncLogWriterFactory { nowMs }.open(directory, config(), "remote")

            main.counter("main.session", 1L)
            remote.counter("remote.session", 1L)
            assertTrue(main.flushBlocking())
            assertTrue(remote.flushBlocking())
            assertTrue(main.close())
            assertTrue(remote.close())

            val parsed = sessionLogFiles(directory).map { file -> requireNotNull(SessionLogName.parse(file.name)) }
            assertEquals(2, parsed.size)
            assertEquals(1, parsed.map { it.runId }.distinct().size)
            assertTrue(parsed.all { it.dailySessionIndex == 0L })
            assertEquals(listOf(0L, 1L), parsed.map { it.segmentIndex }.sorted())
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
            val writer = AsyncLogWriterFactory { nowMs }.open(directory, config(), "main")
            nowMs += 3L * 24L * 60L * 60L * 1_000L

            repeat(32) { index -> writer.counter("after.midnight.$index", index.toLong()) }
            assertTrue(writer.close())

            val parsed = requireNotNull(SessionLogName.parse(sessionLogFiles(directory).single().name))
            assertEquals(expectedDate, parsed.localDate)
            assertEquals(0L, parsed.dailySessionIndex)
            assertEquals(0L, parsed.segmentIndex)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun closeDrainsAcceptedQueueBeforeReturning() {
        val directory = Files.createTempDirectory("jankhunter-close-drain").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(
                directory,
                JankHunterConfig.builder().maxQueueSize(512).flushIntervalMs(60_000L).build(),
                "main",
            )
            repeat(128) { index -> writer.counter("close.queue.$index", index.toLong()) }

            assertTrue(writer.close())

            val text = logFileText(sessionLogFiles(directory).single())
            assertTrue(text.contains("close.queue.0"))
            assertTrue(text.contains("close.queue.127"))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun exactAdmissionHonorsDictionaryHardLimitsAndReportsOverflow() {
        val directory = Files.createTempDirectory("jankhunter-exact-dictionary").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(
                directory,
                JankHunterConfig.builder()
                    .exactEventCollectionEnabled(true)
                    .maxDictionaryEntries(1)
                    .maxDictionaryValueBytes(128)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            val first = "first.metric"
            val second = "second.metric.with.a.full.name"
            writer.counter(first, 1L)
            writer.counter(second, 1L)

            assertTrue(writer.close())

            val file = sessionLogFiles(directory).single()
            val text = logFileText(file)
            assertTrue(text.contains(first))
            assertFalse(text.contains(second))
            val quality = qualityCounters(file)
            assertTrue((quality[QualityCounterId.DICTIONARY_OVERFLOW_TOTAL] ?: 0L) >= 1L)
            assertEquals(0L, quality[QualityCounterId.DICTIONARY_VALUE_TRUNCATED_TOTAL] ?: 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun enabledInternalLimitStopsAtTheTotalJhlogArchiveBoundary() {
        val directory = Files.createTempDirectory("jankhunter-size-limit").toFile()
        val limit = 1024L * 1024L
        try {
            val writer = AsyncLogWriterFactory().open(
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

            val files = sortedSessionLogFiles(directory)
            assertEquals(1, files.size)
            assertTrue(files.single().length() <= limit)
            assertEquals(Jhlog.SEGMENT_END_STORAGE_BUDGET, segmentEndReason(files.single()))
            val quality = qualityCounters(files.single())
            assertEquals(0L, quality[QualityCounterId.EVENT_LOST_AFTER_SIZE_LIMIT_TOTAL] ?: 0L)
            assertTrue((quality[QualityCounterId.EVENT_LOST_AFTER_STORAGE_BUDGET_TOTAL] ?: 0L) > 0L)
            assertTrue((quality[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L) < 12_000L)
            val growth = requireNotNull(writer.logGrowthSummary())
            assertEquals(1, growth.recentSessions.size)
            assertEquals(1_048_576L, growth.recentSessions.single().configuredLimitBytes)
            assertTrue(growth.recentSessions.single().maximumRetainedBytes <= limit)
            assertEquals(1, growthHistoryFiles(directory).size)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun explicitGrowthWritePersistsCurrentSnapshotIntoTheServiceHistory() {
        val directory = Files.createTempDirectory("jankhunter-growth-write").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            writer.counter("growth.snapshot.probe", 1L)

            assertTrue(writer.writeLogGrowthSummaryBlocking())

            val file = sessionLogFiles(directory).single()
            assertEquals(1, growthHistoryFiles(directory).size)
            val current = requireNotNull(writer.logGrowthSummary()?.currentSession)
            assertEquals(file.length(), current.maximumRetainedBytes)
            assertEquals(file.length(), current.generatedBytes)
            assertTrue(writer.close())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun ordinaryFlushRefreshesEmbeddedGrowthCheckpoint() {
        val directory = Files.createTempDirectory("jankhunter-growth-periodic").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(
                directory,
                JankHunterConfig.builder().flushIntervalMs(1L).build(),
                "main",
            )
            writer.counter("growth.periodic.probe", 1L)

            assertTrue(writer.flushBlocking())

            val file = sessionLogFiles(directory).single()
            val liveRecords = recordPayloads(file, Jhlog.TYPE_LOG_GROWTH).count { payload ->
                readUvarint(payload.bytes, payload.offset)?.value == Jhlog.LOG_GROWTH_LIVE
            }
            assertTrue(liveRecords >= 2)
            assertTrue(writer.close())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun captureSnapshotSealsExactFrontierAndContinuesInNextSegment() {
        val directory = Files.createTempDirectory("jankhunter-export-snapshot").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            writer.counter("snapshot.before", 1L)

            val snapshot = requireNotNull(writer.captureSnapshotBlocking())

            assertEquals(1, snapshot.logPaths.size)
            val sealed = File(snapshot.logPaths.single())
            assertTrue(sealed.exists())
            assertEquals(Jhlog.SEGMENT_END_ROTATION, segmentEndReason(sealed))
            assertEquals(1L, qualityCounters(sealed)[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)

            writer.counter("snapshot.after", 1L)
            assertTrue(writer.close())

            val files = sortedSessionLogFiles(directory)
            assertEquals(2, files.size)
            assertEquals(0L, fileSegmentHeader(files[0]).segmentIndex)
            assertEquals(1L, fileSegmentHeader(files[1]).segmentIndex)
            assertEquals(Jhlog.SEGMENT_END_SHUTDOWN, segmentEndReason(files[1]))
            assertEquals(2L, qualityCounters(files[1])[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)
            assertEquals(files[0].absolutePath, sealed.absolutePath)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun storageSwitchConsolidatesBootstrapAndContinuesTheSameSession() {
        val root = Files.createTempDirectory("jankhunter-storage-switch").toFile()
        try {
            val bootstrap = File(root, "bootstrap")
            val target = TestBinaryStorage(File(root, "target"))
            val writer = AsyncLogWriterFactory().open(bootstrap, config(), "main")
            writer.counter("storage.switch.before", 1L)

            assertEquals(
                JankHunterStorageSwitchResult.SWITCHED,
                writer.switchBinaryStorageBlocking(target, timeoutMs = 5_000L),
            )

            writer.counter("storage.switch.after", 1L)
            assertTrue(writer.close())

            assertTrue(sessionLogFiles(bootstrap).isEmpty())
            val files = sortedSessionLogFiles(target.directory)
            assertEquals(2, files.size)
            assertLosslessSegmentChain(files, Long.MAX_VALUE)
            assertEquals(2, files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
            val first = fileSegmentHeader(files[0])
            val second = fileSegmentHeader(files[1])
            assertArrayEquals(first.runId, second.runId)
            assertArrayEquals(first.sessionId, second.sessionId)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun repeatedStorageSwitchConsolidatesTheWholeSessionIntoTheLatestTarget() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-repeated").toFile()
        try {
            val bootstrap = File(root, "bootstrap")
            val firstTarget = TestBinaryStorage(File(root, "first"))
            val secondTarget = TestBinaryStorage(File(root, "second"))
            val writer = AsyncLogWriterFactory().open(bootstrap, config(), "main")
            writer.counter("storage.switch.bootstrap", 1L)

            assertEquals(
                JankHunterStorageSwitchResult.SWITCHED,
                writer.switchBinaryStorageBlocking(firstTarget, timeoutMs = 5_000L),
            )
            writer.counter("storage.switch.first", 1L)
            assertEquals(
                JankHunterStorageSwitchResult.SWITCHED,
                writer.switchBinaryStorageBlocking(secondTarget, timeoutMs = 5_000L),
            )
            writer.counter("storage.switch.second", 1L)
            assertTrue(writer.close())

            assertTrue(sessionLogFiles(bootstrap).isEmpty())
            assertTrue(firstTarget.logFiles().isEmpty())
            val files = sortedSessionLogFiles(secondTarget.directory)
            assertEquals(3, files.size)
            assertLosslessSegmentChain(files, Long.MAX_VALUE)
            assertEquals(3, files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun switchingToTheActiveStorageIsANoOpWithoutSegmentRotation() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-noop").toFile()
        try {
            val target = TestBinaryStorage(File(root, "target"))
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder().binaryStorage(target).build(),
                "main",
            )
            writer.counter("storage.switch.before.noop", 1L)

            assertEquals(
                JankHunterStorageSwitchResult.ALREADY_ACTIVE,
                writer.switchBinaryStorageBlocking(target),
            )

            writer.counter("storage.switch.after.noop", 1L)
            assertTrue(writer.close())
            val file = target.logFiles().single()
            assertEquals(2, recordPayloads(file, Jhlog.TYPE_COUNTER).size)
            assertEquals(0L, fileSegmentHeader(file).segmentIndex)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun storageSwitchAfterWriterCloseReportsClosedAdmission() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-closed").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(File(root, "bootstrap"), config(), "main")
            writer.counter("storage.switch.before.close", 1L)
            assertTrue(writer.close())

            assertEquals(
                JankHunterStorageSwitchResult.NOT_ACCEPTING,
                writer.switchBinaryStorageBlocking(TestBinaryStorage(File(root, "target"))),
            )
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun failedStorageSwitchKeepsCollectingInThePreviousStorage() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-failure").toFile()
        try {
            val bootstrap = File(root, "bootstrap")
            val writer = AsyncLogWriterFactory().open(bootstrap, config(), "main")
            writer.counter("storage.switch.before.failure", 1L)

            assertEquals(
                JankHunterStorageSwitchResult.FAILED,
                writer.switchBinaryStorageBlocking(
                    ThrowingOpenBinaryStorage(File(root, "failing-target")),
                    timeoutMs = 5_000L,
                ),
            )

            writer.counter("storage.switch.after.failure", 1L)
            assertTrue(writer.close())
            val files = sortedSessionLogFiles(bootstrap)
            assertEquals(2, files.size)
            assertLosslessSegmentChain(files, Long.MAX_VALUE)
            assertEquals(2, files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun storageSwitchDoesNotCloseEventAdmissionWhileTargetIsOpening() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-live-admission").toFile()
        try {
            val bootstrap = File(root, "bootstrap")
            val target = BlockingOpenBinaryStorage(File(root, "target"))
            val writer = AsyncLogWriterFactory().open(bootstrap, config(), "main")
            writer.counter("storage.switch.before.blocked.open", 1L)
            val result = AtomicReference<JankHunterStorageSwitchResult>()
            val switchThread = Thread {
                result.set(writer.switchBinaryStorageBlocking(target, timeoutMs = 5_000L))
            }.apply { start() }

            assertTrue(target.awaitOpen())
            writer.counter("storage.switch.while.blocked.open", 1L)
            target.releaseOpen()
            switchThread.join(5_000L)

            assertFalse(switchThread.isAlive)
            assertEquals(JankHunterStorageSwitchResult.SWITCHED, result.get())
            writer.counter("storage.switch.after.blocked.open", 1L)
            assertTrue(writer.close())
            assertTrue(sessionLogFiles(bootstrap).isEmpty())
            val files = sortedSessionLogFiles(File(root, "target"))
            assertLosslessSegmentChain(files, Long.MAX_VALUE)
            assertEquals(3, files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun storageSwitchRecoversClosedBootstrapLogsFromPreviousWriterInstance() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-bootstrap-recovery").toFile()
        try {
            val bootstrap = File(root, "bootstrap")
            AsyncLogWriterFactory().open(bootstrap, config(), "main").also { previous ->
                previous.counter("storage.switch.previous.bootstrap", 1L)
                assertTrue(previous.close())
            }
            val target = TestBinaryStorage(File(root, "target"))
            val current = AsyncLogWriterFactory().open(bootstrap, config(), "main")
            current.counter("storage.switch.current.bootstrap", 1L)

            assertEquals(
                JankHunterStorageSwitchResult.SWITCHED,
                current.switchBinaryStorageBlocking(target, timeoutMs = 5_000L),
            )
            current.counter("storage.switch.current.target", 1L)
            assertTrue(current.close())

            assertTrue(sessionLogFiles(bootstrap).isEmpty())
            assertEquals(3, target.logFiles().sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun nextWriterRecoversExternalSegmentsRegisteredBeforeFailedHandoff() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-external-recovery").toFile()
        try {
            val metadata = File(root, "metadata")
            val firstStorage = TestBinaryStorage(File(root, "first"))
            val first = AsyncLogWriterFactory().open(
                metadata,
                JankHunterConfig.builder().binaryStorage(firstStorage).build(),
                "main",
            )
            first.counter("storage.switch.external.before.failure", 1L)
            assertEquals(
                JankHunterStorageSwitchResult.FAILED,
                first.switchBinaryStorageBlocking(
                    ThrowingOpenBinaryStorage(File(root, "failing")),
                    timeoutMs = 5_000L,
                ),
            )
            first.counter("storage.switch.external.after.failure", 1L)
            assertTrue(first.close())

            val latestStorage = TestBinaryStorage(File(root, "latest"))
            val next = AsyncLogWriterFactory().open(metadata, config(), "main")
            next.counter("storage.switch.recovery.bootstrap", 1L)
            assertEquals(
                JankHunterStorageSwitchResult.SWITCHED,
                next.switchBinaryStorageBlocking(latestStorage, timeoutMs = 5_000L),
            )
            assertTrue(next.close())

            assertTrue(firstStorage.logFiles().isEmpty())
            assertEquals(3, latestStorage.logFiles().sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun storageSwitchNeverMigratesAnotherLiveProcessSegment() {
        val root = Files.createTempDirectory("jankhunter-storage-switch-live-process").toFile()
        try {
            val bootstrap = File(root, "bootstrap")
            val remote = AsyncLogWriterFactory().open(bootstrap, config(), "remote")
            remote.counter("storage.switch.remote.before", 1L)
            assertTrue(remote.flushBlocking(5_000L))
            val main = AsyncLogWriterFactory().open(bootstrap, config(), "main")
            main.counter("storage.switch.main", 1L)
            val target = TestBinaryStorage(File(root, "target"))

            assertEquals(
                JankHunterStorageSwitchResult.SWITCHED,
                main.switchBinaryStorageBlocking(target, timeoutMs = 5_000L),
            )

            assertTrue(sessionLogFiles(bootstrap).isNotEmpty())
            remote.counter("storage.switch.remote.after", 1L)
            assertTrue(remote.close())
            assertTrue(main.close())
            assertEquals(2, sessionLogFiles(bootstrap).sumOf { file ->
                recordPayloads(file, Jhlog.TYPE_COUNTER).size
            })
            assertEquals(1, target.logFiles().sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun scopedGrowthHistoryRemovesThePreReleaseUnscopedStore() {
        val directory = Files.createTempDirectory("jankhunter-growth-schema-cleanup").toFile()
        try {
            val obsolete = File(directory, "jh-log-growth.bin").apply { writeBytes(byteArrayOf(1, 2, 3)) }
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            writer.counter("growth.schema.cleanup", 1L)

            assertTrue(writer.close())
            assertFalse(obsolete.exists())
            assertEquals(1, growthHistoryFiles(directory).size)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun completeProcessRosterRemovesOnlyObsoleteScopedGrowthHistory() {
        val directory = Files.createTempDirectory("jankhunter-growth-process-cleanup").toFile()
        try {
            val retainedRemote = File(directory, LogGrowthHistoryStore.fileName("remote"))
                .apply { writeBytes(byteArrayOf(1)) }
            val obsolete = File(directory, LogGrowthHistoryStore.fileName("renamed-remote"))
                .apply { writeBytes(byteArrayOf(2)) }
            val similarlyNamed = File(directory, "jh-log-growth.manual.bin")
                .apply { writeBytes(byteArrayOf(3)) }

            val writer = AsyncLogWriterFactory().open(
                directory = directory,
                config = config(),
                processName = "main",
                expectedProcesses = setOf("main", "remote"),
                rosterDeclarationComplete = true,
                onTerminalStop = { _, _, _ -> },
            )

            writer.counter("growth.process.cleanup", 1L)
            assertTrue(writer.close())
            assertTrue(File(directory, LogGrowthHistoryStore.fileName("main")).isFile)
            assertTrue(retainedRemote.isFile)
            assertFalse(obsolete.exists())
            assertTrue(similarlyNamed.isFile)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun incompleteProcessRosterPreservesObsoleteScopedGrowthHistory() {
        val directory = Files.createTempDirectory("jankhunter-growth-incomplete-roster").toFile()
        try {
            val obsolete = File(directory, LogGrowthHistoryStore.fileName("renamed-remote"))
                .apply { writeBytes(byteArrayOf(1)) }

            val writer = AsyncLogWriterFactory().open(
                directory = directory,
                config = config(),
                processName = "main",
                expectedProcesses = setOf("main"),
                rosterDeclarationComplete = false,
                onTerminalStop = { _, _, _ -> },
            )

            assertTrue(writer.close())
            assertTrue(obsolete.isFile)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun completeRosterMissingCurrentProcessFailsSafeDuringGrowthHistoryCleanup() {
        val directory = Files.createTempDirectory("jankhunter-growth-invalid-roster").toFile()
        try {
            val obsolete = File(directory, LogGrowthHistoryStore.fileName("renamed-remote"))
                .apply { writeBytes(byteArrayOf(1)) }

            val writer = AsyncLogWriterFactory().open(
                directory = directory,
                config = config(),
                processName = "main",
                expectedProcesses = setOf("remote"),
                rosterDeclarationComplete = true,
                onTerminalStop = { _, _, _ -> },
            )

            assertTrue(writer.close())
            assertTrue(obsolete.isFile)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun disabledGrowthAnalyticsCreatesNoServiceFile() {
        val directory = Files.createTempDirectory("jankhunter-growth-disabled").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(
                directory,
                JankHunterConfig.builder()
                    .logGrowthAnalyticsEnabled(false)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writer.counter("growth.disabled.probe", 1L)

            assertFalse(writer.writeLogGrowthSummaryBlocking())
            assertTrue(writer.close())

            assertEquals(null, writer.logGrowthSummary())
            assertTrue(growthHistoryFiles(directory).isEmpty())
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun disabledInternalLimitLeavesBuiltInStorageUnlimited() {
        val directory = Files.createTempDirectory("jankhunter-size-limit-disabled").toFile()
        val disabledLimit = 1024L * 1024L
        try {
            val writer = AsyncLogWriterFactory().open(
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

            val file = sessionLogFiles(directory).single()
            assertTrue(
                "disabled internal limit still capped the file: ${file.length()} <= $disabledLimit",
                file.length() > disabledLimit,
            )
            assertEquals(Jhlog.SEGMENT_END_SHUTDOWN, segmentEndReason(file))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun externalSegmentLimitRotatesWithinTheLargerArchiveBudget() {
        val root = Files.createTempDirectory("jankhunter-external-limit").toFile()
        val storageLimit = 12L * 1024L
        try {
            val storage = TestBinaryStorage(File(root, "storage"), fileSizeLimitBytes = storageLimit)
            val writer = AsyncLogWriterFactory().open(
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

            val files = sortedSessionLogFiles(storage.directory)
            assertTrue(
                "expected the external boundary to rotate the session; " +
                    "files=${files.map { it.name to it.length() }} opened=${storage.openedNames}",
                files.size > 1,
            )
            assertLosslessSegmentChain(files, storageLimit)
            assertEquals(2_000, files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
            val protected = storage.cleanupProtectedPaths.flatten().toSet()
            assertTrue(files.all { file -> file.absolutePath in protected })
            val growth = requireNotNull(writer.logGrowthSummary()).recentSessions.single()
            assertEquals(1_048_576L, growth.configuredLimitBytes)
            assertFalse(growth.reachedLimit)
            assertEquals(files.size.toLong() - 1L, growth.segmentRotationCount)
            assertTrue(growth.maximumRetainedBytes > storageLimit)
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
            val writer = AsyncLogWriterFactory().open(
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

            val files = sortedSessionLogFiles(storage.directory)
            assertTrue(
                "expected the external boundary to rotate the session; " +
                    "files=${files.map { it.name to it.length() }} opened=${storage.openedNames}",
                files.size > 1,
            )
            assertLosslessSegmentChain(files, storageLimit)
            assertEquals(2_000, files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size })
            val protected = storage.cleanupProtectedPaths.flatten().toSet()
            assertTrue(files.all { file -> file.absolutePath in protected })
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun recordLargerThanAnEmptySegmentStopsOnceWithExplicitQualityEvidence() {
        val root = Files.createTempDirectory("jankhunter-oversized-segment-record").toFile()
        val storageLimit = 10L * 1024L
        try {
            val storage = TestBinaryStorage(File(root, "storage"), fileSizeLimitBytes = storageLimit)
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .sessionLogSizeLimitEnabled(false)
                    .maxDictionaryValueBytes(32_768)
                    .maxQueueSize(16)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            val oversizedName = "oversized." + "x".repeat(20_000)
            writer.counter(oversizedName, 1L)

            assertTrue(writer.close(timeoutMs = 5_000L))

            val files = sortedSessionLogFiles(storage.directory)
            assertEquals(2, files.size)
            assertTrue(files.all { file -> file.length() <= storageLimit })
            assertEquals(Jhlog.SEGMENT_END_ROTATION, segmentEndReason(files.first()))
            assertEquals(Jhlog.SEGMENT_END_SIZE_LIMIT, segmentEndReason(files.last()))
            val quality = qualityCounters(files.last())
            assertEquals(1L, quality[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
            assertEquals(0L, quality[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)
            assertEquals(1L, quality[QualityCounterId.EVENT_LOST_AFTER_SIZE_LIMIT_TOTAL] ?: 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun hardArchiveBudgetSealsRunAndReportsStorageBudgetExhausted() {
        val root = Files.createTempDirectory("jankhunter-archive-budget").toFile()
        val archiveLimit = 64L * 1024L
        try {
            val storage = TestBinaryStorage(
                directory = File(root, "storage"),
                archivesSizeLimitBytes = archiveLimit,
            )
            val terminal = CountDownLatch(1)
            val terminalReason = AtomicInteger()
            val writer = AsyncLogWriterFactory().open(
                directory = File(root, "leases"),
                config = JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .sessionLogSizeLimitEnabled(false)
                    .maxQueueSize(512)
                    .flushIntervalMs(1L)
                    .build(),
                processName = "main",
                onTerminalStop = { _, reason, _ ->
                    terminalReason.set(reason)
                    terminal.countDown()
                },
            )

            writeIncompressibleCounters(writer)

            assertTrue(terminal.await(10L, TimeUnit.SECONDS))
            assertTrue(writer.close(timeoutMs = 5_000L))
            val files = sortedSessionLogFiles(storage.directory)
            assertTrue(files.isNotEmpty())
            assertTrue(files.sumOf(File::length) <= archiveLimit)
            assertEquals(Jhlog.SEGMENT_END_STORAGE_BUDGET, segmentEndReason(files.last()))
            assertEquals(QualityCounterId.REASON_STORAGE_BUDGET, terminalReason.get())
            val quality = qualityCounters(files.last())
            assertTrue((quality[QualityCounterId.EVENT_LOST_AFTER_STORAGE_BUDGET_TOTAL] ?: 0L) > 0L)
            assertEquals(0L, quality[QualityCounterId.WRITER_IO_ERROR_TOTAL] ?: 0L)
            val growth = requireNotNull(writer.logGrowthSummary()).recentSessions.single()
            assertEquals(archiveLimit, growth.configuredLimitBytes)
            assertTrue(growth.reachedLimit)
            assertEquals(1L, growth.limitReachedCount)
            assertEquals(0L, growth.segmentRotationCount)
            assertTrue(growth.maximumRetainedBytes <= archiveLimit)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun nextRunEvictsPreviousRunAsAWholeAndRecordsEvidence() {
        val root = Files.createTempDirectory("jankhunter-archive-cycle").toFile()
        val archiveLimit = 16L * 1024L
        try {
            val storageDirectory = File(root, "storage").apply { mkdirs() }
            val oldRun = ByteArray(16) { 3 }
            val oldFirst = File(storageDirectory, SessionLogName.create("2027-01-01", oldRun, 0L, 0L))
                .apply { writeBytes(ByteArray(8_000)) }
            val oldSecond = File(storageDirectory, SessionLogName.create("2027-01-01", oldRun, 0L, 1L))
                .apply { writeBytes(ByteArray(8_300)) }
            val heap = File(storageDirectory, "retained-large.hprof").apply { writeBytes(ByteArray(128 * 1024)) }
            val storage = TestBinaryStorage(
                directory = storageDirectory,
                archivesSizeLimitBytes = archiveLimit,
            )
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .sessionLogSizeLimitEnabled(false)
                    .flushIntervalMs(1L)
                    .build(),
                "main",
            )

            writer.counter("new.run", 1L)
            assertTrue(writer.close())

            assertFalse(oldFirst.exists())
            assertFalse(oldSecond.exists())
            assertTrue(heap.exists())
            val current = storage.logFiles().single()
            val quality = qualityCounters(current)
            assertEquals(1L, quality[QualityCounterId.ARCHIVE_EVICTED_RUN_TOTAL] ?: 0L)
            assertEquals(2L, quality[QualityCounterId.ARCHIVE_EVICTED_SEGMENT_TOTAL] ?: 0L)
            assertEquals(16_300L, quality[QualityCounterId.ARCHIVE_EVICTED_BYTES_TOTAL] ?: 0L)
            val growth = requireNotNull(writer.logGrowthSummary()).recentSessions.single()
            assertFalse(growth.reachedLimit)
            assertEquals(16_300L, growth.archiveEvictedBytes)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun ioFailureDoesNotOpenReplacementOrReplayEvents() {
        val root = Files.createTempDirectory("jankhunter-io-failure").toFile()
        try {
            val storage = FailingBinaryStorage(File(root, "storage"), failAfterBytes = 1_024L)
            val writer = AsyncLogWriterFactory().open(
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
            val writer = AsyncLogWriterFactory().open(
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
            val storage = TestBinaryStorage(storageDirectory, collideFirstOpen = true)

            val writer = AsyncLogWriterFactory { nowMs }.open(
                File(root, "leases"),
                JankHunterConfig.builder().binaryStorage(storage).build(),
                "main",
            )
            writer.counter("after.collision", 1L)
            assertTrue(writer.close())

            assertEquals(listOf(0L, 1L), storage.openedNames.map { requireNotNull(SessionLogName.parse(it)).segmentIndex })
            assertTrue(storage.openedNames.all { requireNotNull(SessionLogName.parse(it)).dailySessionIndex == 0L })
            val written = storage.logFiles().single { file -> SessionLogName.parse(file.name)?.segmentIndex == 1L }
            assertTrue(logFileText(written).contains("after.collision"))
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
            SessionLogAllocator.reserve(
                leaseDirectory,
                date,
                ByteArray(16) { 1 },
                dailySessionIndex = 0L,
            ).close()

            val writer = AsyncLogWriterFactory { nowMs }.open(
                leaseDirectory,
                JankHunterConfig.builder().binaryStorage(storage).build(),
                "main",
            )
            writer.counter("external.zero", 1L)
            assertTrue(writer.close())

            assertEquals(listOf(0L), storage.openedNames.map { requireNotNull(SessionLogName.parse(it)).segmentIndex })
            assertEquals(0L, requireNotNull(SessionLogName.parse(storage.logFiles().single().name)).dailySessionIndex)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun invalidMetricsStayOutOfTheDataStreamAndReachQualitySnapshot() {
        val directory = Files.createTempDirectory("jankhunter-invalid-metric").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            writer.counter("invalid.counter", -1L)
            writer.gauge("invalid.gauge", -1L)
            writer.counter("valid.counter", 1L)
            assertTrue(writer.close())

            val file = sessionLogFiles(directory).single()
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
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .exactEventCollectionEnabled(false)
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
            assertEquals(0L, fileSegmentHeader(file).requiredFeatures and Jhlog.FEATURE_EXACT_EVENT_ADMISSION)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun producerContextUpdatesDoNotConsumeQueueCapacity() {
        val root = Files.createTempDirectory("jankhunter-context-update-burst").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .maxQueueSize(2048)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )

            writer.counter("bulk.first", 1L)
            assertTrue(storage.awaitOpen())
            repeat(199) { index ->
                writer.updateProducerContext("Startup", "App", (index + 1).toLong())
            }
            storage.releaseOpen()
            assertTrue(storage.awaitWriterCreated())

            assertTrue(writer.close(timeoutMs = 5_000L))
            val file = storage.logFiles().single()
            assertTrue(logFileText(file).contains("bulk.first"))
            val counters = qualityCounters(file)
            assertEquals(0L, counters[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun exactAdmissionBackpressuresAndPreservesEveryAcceptedEvent() {
        val root = Files.createTempDirectory("jankhunter-exact-backpressure").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .exactEventCollectionEnabled(true)
                    .maxQueueSize(1)
                    .backgroundAdmissionWaitMs(5_000L)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )

            writer.counter("exact.first", 1L)
            assertTrue(storage.awaitOpen())
            val secondCompleted = AtomicBoolean(false)
            val producer = Thread {
                writer.counter("exact.second", 2L)
                secondCompleted.set(true)
            }.also(Thread::start)
            assertTrue(awaitThreadBlocked(producer))

            storage.releaseOpen()
            producer.join(5_000L)
            assertFalse(producer.isAlive)
            assertTrue(secondCompleted.get())
            assertTrue(storage.awaitWriterCreated())
            assertTrue(writer.close(timeoutMs = 5_000L))

            val file = storage.logFiles().single()
            val text = logFileText(file)
            assertTrue(text.contains("exact.first"))
            assertTrue(text.contains("exact.second"))
            val counters = qualityCounters(file)
            assertEquals(0L, counters[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
            assertEquals(2L, counters[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
            assertEquals(2L, counters[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)
            assertTrue((counters[QualityCounterId.WRITER_BACKPRESSURE_COUNT] ?: 0L) > 0L)
            assertTrue((counters[QualityCounterId.WRITER_BACKPRESSURE_NANOS] ?: 0L) > 0L)
            assertTrue(fileSegmentHeader(file).requiredFeatures and Jhlog.FEATURE_EXACT_EVENT_ADMISSION != 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun mainThreadAdmissionNeverWaitsForAFullQueueByDefault() {
        val root = Files.createTempDirectory("jankhunter-main-admission").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .exactEventCollectionEnabled(true)
                    .maxQueueSize(1)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writer.counter("main.first", 1L)
            assertTrue(storage.awaitOpen())

            val completed = AtomicBoolean(false)
            val producer = Thread({
                writer.counter("main.rejected", 2L)
                completed.set(true)
            }, "main")
            producer.start()
            producer.join(1_000L)

            assertFalse("main-thread producer blocked on collector admission", producer.isAlive)
            assertTrue(completed.get())
            storage.releaseOpen()
            assertTrue(writer.close(timeoutMs = 5_000L))

            val file = storage.logFiles().single()
            assertFalse(logFileText(file).contains("main.rejected"))
            val counters = qualityCounters(file)
            assertEquals(1L, counters[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
            assertEquals(1L, counters[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun exactAdmissionPreservesMillionEventConcurrentBurstAcrossRotations() {
        val root = Files.createTempDirectory("jankhunter-exact-million-burst").toFile()
        val physicalLimit = 512L * 1024L
        val producerCount = 16
        val eventsPerProducer = 62_500
        val expectedEvents = producerCount.toLong() * eventsPerProducer
        try {
            val storage = TestBinaryStorage(File(root, "storage"), fileSizeLimitBytes = physicalLimit)
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .exactEventCollectionEnabled(true)
                    .maxQueueSize(64)
                    .backgroundAdmissionWaitMs(30_000L)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            val start = CountDownLatch(1)
            val done = CountDownLatch(producerCount)
            val producers = List(producerCount) { producer ->
                Thread({
                    start.await()
                    repeat(eventsPerProducer) { event ->
                        writer.counter(
                            "exact.stress.$producer.${event and 63}",
                            producer.toLong() * eventsPerProducer + event,
                        )
                    }
                    done.countDown()
                }, "JankHunterExactStress-$producer")
            }

            producers.forEach(Thread::start)
            start.countDown()
            assertTrue("million-event producers timed out", done.await(60L, TimeUnit.SECONDS))
            producers.forEach { producer -> producer.join(2_000L) }
            assertTrue(producers.none(Thread::isAlive))
            assertTrue(writer.close(timeoutMs = 60_000L))

            val files = sortedSessionLogFiles(storage.directory)
            assertTrue("million-event burst did not exercise rotation", files.size > 1)
            assertLosslessSegmentChain(files, physicalLimit)
            assertEquals(
                expectedEvents,
                files.sumOf { file -> recordPayloads(file, Jhlog.TYPE_COUNTER).size.toLong() },
            )
            val quality = qualityCounters(files.last())
            assertEquals(expectedEvents, quality[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
            assertEquals(expectedEvents, quality[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)
            assertEquals(0L, quality[QualityCounterId.QUEUE_FULL_TOTAL] ?: 0L)
            assertEquals(0L, quality[QualityCounterId.EVENT_LOST_AFTER_IO_TOTAL] ?: 0L)
            assertEquals(0L, quality[QualityCounterId.EVENT_LOST_AFTER_SIZE_LIMIT_TOTAL] ?: 0L)
            assertTrue((quality[QualityCounterId.WRITER_BACKPRESSURE_COUNT] ?: 0L) > 0L)
            assertTrue((quality[QualityCounterId.WRITER_BACKPRESSURE_NANOS] ?: 0L) > 0L)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun exactBlockingFlushWaitsPastCallerTimeoutWithoutLosingData() {
        val root = Files.createTempDirectory("jankhunter-exact-flush-frontier").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .exactEventCollectionEnabled(true)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writer.counter("exact.flush.frontier", 1L)
            assertTrue(storage.awaitOpen())
            val succeeded = AtomicBoolean(false)
            val flushThread = Thread {
                succeeded.set(writer.flushBlocking(timeoutMs = 1L))
            }.also(Thread::start)
            assertTrue(awaitThreadBlocked(flushThread))

            storage.releaseOpen()
            flushThread.join(5_000L)
            assertFalse(flushThread.isAlive)
            assertTrue(succeeded.get())
            assertTrue(writer.close(timeoutMs = 1L))
            assertTrue(logFileText(storage.logFiles().single()).contains("exact.flush.frontier"))
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun exactCloseWaitsPastCallerTimeoutForSealedFrontier() {
        val root = Files.createTempDirectory("jankhunter-exact-close-frontier").toFile()
        try {
            val storage = BlockingOpenBinaryStorage(File(root, "storage"))
            val writer = AsyncLogWriterFactory().open(
                File(root, "leases"),
                JankHunterConfig.builder()
                    .binaryStorage(storage)
                    .exactEventCollectionEnabled(true)
                    .flushIntervalMs(60_000L)
                    .build(),
                "main",
            )
            writer.counter("exact.close.frontier", 1L)
            assertTrue(storage.awaitOpen())
            val succeeded = AtomicBoolean(false)
            val closeThread = Thread {
                succeeded.set(writer.close(timeoutMs = 1L))
            }.also(Thread::start)
            assertTrue(awaitThreadBlocked(closeThread))

            storage.releaseOpen()
            closeThread.join(5_000L)
            assertFalse(closeThread.isAlive)
            assertTrue(succeeded.get())
            val file = storage.logFiles().single()
            assertTrue(logFileText(file).contains("exact.close.frontier"))
            assertEquals(Jhlog.SEGMENT_END_SHUTDOWN, segmentEndReason(file))
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun runtimeCallUsesStableZeroAndNegativeIdsWithoutOwnerDictionaryEntries() {
        val directory = Files.createTempDirectory("jankhunter-stable-runtime-call").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            val batch = RuntimeCallBatch(1).apply {
                add("screen", 0L, "example.Caller.call", 41L, -1L, "example.Callee.call", 3L, 12L, 7L)
            }
            writer.runtimeCalls(batch)
            assertTrue(writer.close())

            val file = sessionLogFiles(directory).single()
            val call = recordPayloads(file, Jhlog.TYPE_RUNTIME_CALL).single()
            val rowCount = readUvarint(call.bytes, call.offset)
            assertEquals(1L, rowCount?.value)
            val screen = readSymbolRef(call.bytes, rowCount!!.nextOffset)
            val caller = readSymbolRef(call.bytes, screen!!.nextOffset)
            val operationId = readUvarint(call.bytes, caller!!.nextOffset)
            val callee = readSymbolRef(call.bytes, operationId!!.nextOffset)
            assertEquals(41L, operationId.value)
            assertNotNull(caller)
            assertTrue(caller.stable)
            assertEquals(0L, caller.id)
            assertNotNull(callee)
            assertTrue(callee!!.stable)
            assertEquals(-1L, callee.id)

            val dictionaryKinds = recordPayloads(file, Jhlog.TYPE_DICTIONARY).mapNotNull { payload ->
                readUvarint(payload.bytes, payload.offset)?.value
            }
            assertFalse("runtime caller/callee created DICT_OWNER", dictionaryKinds.contains(1L))
            assertTrue("runtime caller/callee did not create embedded stable definitions", dictionaryKinds.contains(12L))
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun batchedQueueAccountingCountsLogicalWireRecords() {
        val directory = Files.createTempDirectory("jankhunter-batch-accounting").toFile()
        try {
            val writer = AsyncLogWriterFactory().open(directory, config(), "main")
            writer.runtimeCalls(
                RuntimeCallBatch(2).apply {
                    add(null, 0L, "zero", 0L, 1L, "one", 1L, 1L, 1L)
                    add(null, 1L, "one", 0L, 2L, "two", 1L, 1L, 1L)
                },
            )
            writer.stableCounters(
                StableCounterBatch(3).apply {
                    add(0L, "zero", 2L)
                    add(-1L, "negative", 3L)
                    add(2L, "two", 4L)
                },
            )
            assertTrue(writer.close())

            val file = sessionLogFiles(directory).single()
            val runtimeBlocks = recordPayloads(file, Jhlog.TYPE_RUNTIME_CALL)
            assertEquals(1, runtimeBlocks.size)
            assertEquals(2L, readUvarint(runtimeBlocks.single().bytes, runtimeBlocks.single().offset)?.value)
            val quality = qualityCounters(file)
            assertEquals(5L, quality[QualityCounterId.ACCEPTED_EVENT_TOTAL] ?: 0L)
            assertEquals(5L, quality[QualityCounterId.WRITTEN_EVENT_TOTAL] ?: 0L)
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun binaryWriterRejectsReservedDatabaseIOValues() {
        val directory = Files.createTempDirectory("jankhunter-reserved-io-values").toFile()
        try {
            BinaryLogWriter(File(directory, "strict.jhlog")).use { writer ->
                for (operation in 4L..5L) {
                    assertThrows(IllegalArgumentException::class.java) {
                        writer.io(
                            operation = operation,
                            durationUs = 1L,
                            bytes = 0L,
                            mainThread = false,
                            sourceId = 0L,
                            sourceName = null,
                            outcome = Jhlog.IO_OUTCOME_SUCCESS,
                            bytesKnown = false,
                        )
                    }
                }
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun binaryWriterRejectsStableReferencesWithoutReadableNames() {
        val directory = Files.createTempDirectory("jankhunter-stable-symbol-contract").toFile()
        try {
            BinaryLogWriter(File(directory, "strict.jhlog")).use { writer ->
                assertThrows(IllegalArgumentException::class.java) {
                    writer.worker(
                        workerId = 0x103L,
                        workerName = null,
                        instanceId = 1L,
                        stage = Jhlog.WORKER_STAGE_STARTED,
                        outcome = Jhlog.WORKER_OUTCOME_UNKNOWN,
                        durationMs = 0L,
                        runAttempt = 0L,
                        generation = 0L,
                        stopReason = 0L,
                        flags = 0L,
                    )
                }
            }
        } finally {
            directory.deleteRecursively()
        }
    }

    @Test
    fun binaryWriterRejectsConflictingNamesForOneStableId() {
        val directory = Files.createTempDirectory("jankhunter-stable-symbol-collision").toFile()
        try {
            BinaryLogWriter(File(directory, "collision.jhlog")).use { writer ->
                writer.stableCounter(0x103L, "example.First.call", 1L)
                assertThrows(IllegalArgumentException::class.java) {
                    writer.stableCounter(0x103L, "example.Second.call", 1L)
                }
            }
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
        return recordPayloads(file, Jhlog.TYPE_SEGMENT_END)
            .lastOrNull()
            ?.let { payload -> readUvarint(payload.bytes, payload.offset)?.value }
    }

    private fun assertLosslessSegmentChain(files: List<File>, physicalLimitBytes: Long) {
        val headers = files.map(::fileSegmentHeader)
        assertTrue(headers.isNotEmpty())
        val first = headers.first()
        headers.forEachIndexed { index, header ->
            val file = files[index]
            assertTrue(
                "physical limit exceeded: ${file.length()} > $physicalLimitBytes for ${file.name}",
                file.length() <= physicalLimitBytes,
            )
            assertArrayEquals(first.runId, header.runId)
            assertArrayEquals(first.processInstanceId, header.processInstanceId)
            assertArrayEquals(first.sessionId, header.sessionId)
            assertEquals(index.toLong(), header.segmentIndex)
            val expectedPredecessor = if (index == 0) {
                ByteArray(0)
            } else {
                MessageDigest.getInstance("SHA-256").digest(files[index - 1].readBytes())
            }
            assertArrayEquals("invalid predecessor digest for ${file.name}", expectedPredecessor, header.previousSegmentDigest)
            val expectedReason = if (index == files.lastIndex) {
                Jhlog.SEGMENT_END_SHUTDOWN
            } else {
                Jhlog.SEGMENT_END_ROTATION
            }
            assertEquals("unexpected end reason for ${file.name}", expectedReason, segmentEndReason(file))
        }
    }

    private fun qualityCounters(file: File): Map<Int, Long> {
        val latest = LinkedHashMap<Int, Long>()
        recordPayloads(file, Jhlog.TYPE_QUALITY_SNAPSHOT).forEach { payload ->
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

    private fun readUvarintValues(payload: RecordPayload, count: Int): List<Long> {
        val values = ArrayList<Long>(count)
        var cursor = payload.offset
        repeat(count) {
            val value = requireNotNull(readUvarint(payload.bytes, cursor))
            values += value.value
            cursor = value.nextOffset
        }
        return values
    }

    private fun recordPayloads(file: File, expectedType: Int): List<RecordPayload> {
        val payloads = ArrayList<RecordPayload>()
        committedRawChunks(file.readBytes()).forEach { raw ->
            var offset = 0
            var lastContextScreen: SymbolWire? = null
            var lastContextOwner: SymbolWire? = null
            var lastContextOperationId = 0L
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
                if (flags.value and Jhlog.ENVELOPE_HAS_TIME != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
                if (flags.value and Jhlog.ENVELOPE_HAS_THREAD != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
                var contextScreen: SymbolWire? = null
                var contextOwner: SymbolWire? = null
                var contextOperationId = 0L
                if (flags.value and Jhlog.ENVELOPE_HAS_CONTEXT != 0L) {
                    if (flags.value and Jhlog.ENVELOPE_SAME_CONTEXT != 0L) {
                        contextScreen = lastContextScreen
                        contextOwner = lastContextOwner
                        contextOperationId = lastContextOperationId
                    } else {
                        val presence = readUvarint(raw, cursor) ?: break
                        cursor = presence.nextOffset
                        if (presence.value and Jhlog.CONTEXT_SCREEN != 0L) {
                            contextScreen = readSymbolRef(raw, cursor) ?: return@forEach
                            cursor = contextScreen.nextOffset
                        }
                        if (presence.value and Jhlog.CONTEXT_OWNER != 0L) {
                            contextOwner = readSymbolRef(raw, cursor) ?: return@forEach
                            cursor = contextOwner.nextOffset
                        }
                        if (presence.value and Jhlog.CONTEXT_OPERATION != 0L) {
                            val operationId = readUvarint(raw, cursor) ?: return@forEach
                            contextOperationId = operationId.value
                            cursor = operationId.nextOffset
                        }
                        lastContextScreen = contextScreen
                        lastContextOwner = contextOwner
                        lastContextOperationId = contextOperationId
                    }
                }
                if (flags.value and Jhlog.ENVELOPE_HAS_ATTRIBUTES != 0L) {
                    cursor = readUvarint(raw, cursor)?.nextOffset ?: break
                }
                if (type.value == expectedType.toLong()) {
                    payloads += RecordPayload(raw, cursor, contextOwner, contextScreen, contextOperationId)
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
        while (offset + Jhlog.CHUNK_HEADER_BYTES <= fileBytes.size) {
            if (!fileBytes.matchesAscii(offset, "JHC1")) break
            val flags = readUInt16Le(fileBytes, offset + 6)
            val storedLength = readUInt32Le(fileBytes, offset + 12).toInt()
            val payloadStart = offset + Jhlog.CHUNK_HEADER_BYTES
            val trailerStart = payloadStart + storedLength
            val chunkEnd = trailerStart + Jhlog.COMMIT_TRAILER_BYTES
            if (storedLength < 0 || trailerStart < payloadStart || chunkEnd > fileBytes.size) break
            if (!fileBytes.matchesAscii(trailerStart, "JHCM")) break
            val stored = fileBytes.copyOfRange(payloadStart, trailerStart)
            chunks += if (flags and Jhlog.CHUNK_FLAG_GZIP != 0) {
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
        val headerFrame = MAGIC_SIZE
        val headerLength = readUInt32Le(bytes, headerFrame).toInt()
        val payloadStart = headerFrame + Int.SIZE_BYTES * 2
        val headerEnd = payloadStart + headerLength
        var cursor = payloadStart
        repeat(3) { cursor = readUvarint(bytes, cursor)?.nextOffset ?: return ByteArray(0) }
        cursor += 16 * 3
        repeat(7) { cursor = readUvarint(bytes, cursor)?.nextOffset ?: return ByteArray(0) }
        val processNameLength = readUvarint(bytes, cursor) ?: return ByteArray(0)
        cursor = processNameLength.nextOffset + processNameLength.value.toInt()
        val namespaceLength = readUvarint(bytes, cursor) ?: return ByteArray(0)
        cursor = namespaceLength.nextOffset
        val end = cursor + namespaceLength.value.toInt()
        if (cursor < payloadStart || end < cursor || end > headerEnd || end > bytes.size) return ByteArray(0)
        return bytes.copyOfRange(cursor, end)
    }

    private fun fileProcessScope(file: File): ProcessScopeWire {
        val bytes = file.readBytes()
        val headerLength = readUInt32Le(bytes, MAGIC_SIZE).toInt()
        val payloadStart = FILE_PREFIX_BYTES
        val headerEnd = payloadStart + headerLength
        var cursor = payloadStart
        repeat(3) { cursor = requireNotNull(readUvarint(bytes, cursor)).nextOffset }
        cursor += 16 * 3
        repeat(7) { cursor = requireNotNull(readUvarint(bytes, cursor)).nextOffset }
        repeat(2) {
            val length = requireNotNull(readUvarint(bytes, cursor))
            cursor = length.nextOffset + length.value.toInt()
            require(cursor <= headerEnd) { "truncated JHLOG header in ${file.name}" }
        }
        val scope = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }.value
        val allowedCount = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }.value
        val fingerprintLength = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }
        val fingerprintEnd = cursor + fingerprintLength.value.toInt()
        require(fingerprintEnd <= headerEnd) { "truncated process scope fingerprint in ${file.name}" }
        return ProcessScopeWire(scope, allowedCount, bytes.copyOfRange(cursor, fingerprintEnd))
    }

    private fun fileSegmentHeader(file: File): SegmentHeaderWire {
        val bytes = file.readBytes()
        val headerLength = readUInt32Le(bytes, MAGIC_SIZE).toInt()
        val payloadStart = FILE_PREFIX_BYTES
        val headerEnd = payloadStart + headerLength
        var cursor = payloadStart
        cursor = requireNotNull(readUvarint(bytes, cursor)).nextOffset
        val requiredFeatures = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }.value
        cursor = requireNotNull(readUvarint(bytes, cursor)).nextOffset
        fun readId(): ByteArray {
            val end = cursor + 16
            require(end <= headerEnd) { "truncated JHLOG header in ${file.name}" }
            return bytes.copyOfRange(cursor, end).also { cursor = end }
        }
        val runId = readId()
        val processInstanceId = readId()
        val sessionId = readId()
        val segmentIndexWire = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }
        repeat(6) { cursor = requireNotNull(readUvarint(bytes, cursor)).nextOffset }
        repeat(2) {
            val length = requireNotNull(readUvarint(bytes, cursor))
            cursor = length.nextOffset + length.value.toInt()
        }
        repeat(2) { cursor = requireNotNull(readUvarint(bytes, cursor)).nextOffset }
        val fingerprintLength = requireNotNull(readUvarint(bytes, cursor))
        cursor = fingerprintLength.nextOffset + fingerprintLength.value.toInt()
        val digestLength = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }
        val digestEnd = cursor + digestLength.value.toInt()
        require(digestEnd <= headerEnd) { "truncated predecessor digest in ${file.name}" }
        val predecessorDigest = bytes.copyOfRange(cursor, digestEnd)
        cursor = digestEnd
        val expectedCount = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }
        val expectedFingerprintLength = requireNotNull(readUvarint(bytes, cursor)).also { cursor = it.nextOffset }
        val expectedFingerprintEnd = cursor + expectedFingerprintLength.value.toInt()
        require(expectedFingerprintEnd <= headerEnd) { "truncated expected process fingerprint in ${file.name}" }
        val expectedFingerprint = bytes.copyOfRange(cursor, expectedFingerprintEnd)
        cursor = expectedFingerprintEnd
        val rosterComplete = requireNotNull(readUvarint(bytes, cursor)).value
        return SegmentHeaderWire(
            runId,
            processInstanceId,
            sessionId,
            segmentIndexWire.value,
            requiredFeatures,
            predecessorDigest,
            expectedCount.value,
            expectedFingerprint,
            rosterComplete == 1L,
        )
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
        private val collideFirstOpen: Boolean = false,
    ) : JankHunterBinaryStorage {
        val openedNames = mutableListOf<String>()
        val cleanupProtectedPaths = mutableListOf<Set<String>>()

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            directory.mkdirs()
            openedNames += fileName
            val file = File(directory, fileName)
            if (collideFirstOpen && openedNames.size == 1) file.writeBytes(byteArrayOf(1))
            return FileBinaryWriter(file)
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

        fun logFiles(): List<File> = sessionLogFiles(directory)
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
            fileArtifact(File(directory, fileName))

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = directory.listFiles().orEmpty().map { file -> file.absolutePath }

        fun logFiles(): List<File> = sessionLogFiles(directory)
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
            fileArtifact(File(directory, fileName))

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = directory.listFiles().orEmpty().map { file -> file.absolutePath }

        fun awaitOpen(): Boolean = openStarted.await(5, TimeUnit.SECONDS)

        fun releaseOpen() {
            allowOpen.countDown()
        }

        fun awaitWriterCreated(): Boolean = writerCreated.await(5, TimeUnit.SECONDS)

        fun logFiles(): List<File> = sessionLogFiles(directory)
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

    private fun awaitThreadBlocked(thread: Thread): Boolean {
        val deadlineNs = System.nanoTime() + TimeUnit.SECONDS.toNanos(5L)
        while (System.nanoTime() < deadlineNs) {
            if (thread.state == Thread.State.WAITING || thread.state == Thread.State.TIMED_WAITING) return true
            if (!thread.isAlive) return false
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
            noOpArtifact(fileName)

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = emptyList()

        fun awaitOpen(): Boolean = openStarted.await(5, TimeUnit.SECONDS)

        fun failOpen() {
            allowFailure.countDown()
        }
    }

    private class ThrowingOpenBinaryStorage(
        private val directory: File,
    ) : JankHunterBinaryStorage {
        override val fileSizeLimitBytes: Long = Long.MAX_VALUE
        override val archivesSizeLimitBytes: Long = Long.MAX_VALUE

        override fun openWriter(fileName: String): JankHunterBinaryWriter {
            throw IOException("injected storage switch failure")
        }

        override fun createArtifact(fileName: String): JankHunterBinaryArtifact =
            fileArtifact(File(directory, fileName))

        override fun cleanup(protectedPaths: Set<String>) = Unit

        override fun listFiles(): List<String> = emptyList()
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
        val contextScreen: SymbolWire? = null,
        val contextOperationId: Long = 0L,
    )

    private data class SymbolWire(
        val stable: Boolean,
        val id: Long,
        val nextOffset: Int,
    )

    private data class SegmentHeaderWire(
        val runId: ByteArray,
        val processInstanceId: ByteArray,
        val sessionId: ByteArray,
        val segmentIndex: Long,
        val requiredFeatures: Long,
        val previousSegmentDigest: ByteArray,
        val expectedProcessCount: Long,
        val expectedProcessFingerprint: ByteArray,
        val rosterDeclarationComplete: Boolean,
    )

    private fun processFingerprint(processes: Set<String>): ByteArray {
        val digest = MessageDigest.getInstance("SHA-256")
        val length = ByteArray(Int.SIZE_BYTES)
        processes.sorted().forEach { processName ->
            val bytes = processName.toByteArray(StandardCharsets.UTF_8)
            length[0] = (bytes.size ushr 24).toByte()
            length[1] = (bytes.size ushr 16).toByte()
            length[2] = (bytes.size ushr 8).toByte()
            length[3] = bytes.size.toByte()
            digest.update(length)
            digest.update(bytes)
        }
        return digest.digest()
    }

    private class ProcessScopeWire(
        val scope: Long,
        val allowedCount: Long,
        val fingerprint: ByteArray,
    ) {
        override fun equals(other: Any?): Boolean =
            other is ProcessScopeWire &&
                scope == other.scope &&
                allowedCount == other.allowedCount &&
                fingerprint.contentEquals(other.fingerprint)

        override fun hashCode(): Int = 31 * (31 * scope.hashCode() + allowedCount.hashCode()) + fingerprint.contentHashCode()

        override fun toString(): String =
            "ProcessScopeWire(scope=$scope, allowedCount=$allowedCount, fingerprint=${fingerprint.contentToString()})"
    }

    private companion object {
        val MAGIC_SIZE = Jhlog.FILE_MAGIC.size
        val FILE_PREFIX_BYTES = MAGIC_SIZE + Int.SIZE_BYTES * 2

        fun sessionLogFiles(directory: File): List<File> {
            return directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
                .orEmpty()
                .toList()
        }

        fun growthHistoryFiles(directory: File): List<File> {
            return directory.listFiles { file ->
                file.isFile && file.name.startsWith("jh-log-growth.") && file.name.endsWith(".bin")
            }.orEmpty().toList()
        }

        fun sortedSessionLogFiles(directory: File): List<File> {
            return sessionLogFiles(directory).sortedWith(
                compareBy<File> { file -> SessionLogName.parse(file.name)?.localDate.orEmpty() }
                    .thenBy { file -> SessionLogName.parse(file.name)?.dailySessionIndex ?: Long.MAX_VALUE }
                    .thenBy { file -> SessionLogName.parse(file.name)?.segmentIndex ?: Long.MAX_VALUE },
            )
        }

        fun noOpArtifact(path: String): JankHunterBinaryArtifact = object : JankHunterBinaryArtifact {
            override val path = path

            override fun commit() = Unit

            override fun abort() = Unit
        }

        fun fileArtifact(file: File): JankHunterBinaryArtifact = object : JankHunterBinaryArtifact {
            override val path: String = file.absolutePath

            override fun commit() = Unit

            override fun abort() {
                file.delete()
            }
        }
    }
}
