package io.jankhunter.runtime.internal.io

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.Build
import io.jankhunter.runtime.JankHunterLogSnapshot
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.charset.StandardCharsets
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import java.util.concurrent.locks.LockSupport
import java.util.concurrent.locks.ReentrantLock
import java.util.zip.CRC32
import kotlin.concurrent.withLock

/** Coordinates a vector snapshot across every live Jank Hunter process without steady polling. */
internal class ProcessLogSnapshotCoordinator private constructor(
    private val context: Context,
    private val directory: File,
    private val participantLease: ProcessSnapshotParticipants.Lease,
    private val captureLocal: () -> JankHunterLogSnapshot?,
) : Closeable {
    private val closed = AtomicBoolean()
    private val responseInFlight = AtomicBoolean()
    private val requesterLock = ReentrantLock()
    private val captureLock = ReentrantLock()
    private val action = "${context.packageName}.$ACTION_SUFFIX"
    private val broadcastPermission = "${context.packageName}.$PERMISSION_SUFFIX"
    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (closed.get() || intent?.action != action) return
            val token = intent.getStringExtra(EXTRA_TOKEN)?.takeIf(::isToken) ?: return
            val requesterId = intent.getStringExtra(EXTRA_REQUESTER)?.takeIf(::isToken) ?: return
            if (requesterId == participantLease.id) return
            val captureAllowed = responseInFlight.compareAndSet(false, true)
            val pendingResult = goAsync()
            val deadlineNs = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(RESPONDER_TIMEOUT_MS)
            val responseThread = Thread(
                {
                    try {
                        android.os.Process.setThreadPriority(android.os.Process.THREAD_PRIORITY_BACKGROUND)
                        if (captureAllowed) {
                            respond(token, deadlineNs)
                        } else {
                            writeResponse(token, participantLease.id, null)
                        }
                    } finally {
                        pendingResult.finish()
                    }
                },
                RESPONSE_THREAD_NAME,
            ).apply { isDaemon = true }
            try {
                responseThread.start()
            } catch (_: Throwable) {
                if (captureAllowed) responseInFlight.set(false)
                pendingResult.finish()
            }
        }
    }

    init {
        val filter = IntentFilter(action)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            context.registerReceiver(
                receiver,
                filter,
                broadcastPermission,
                null,
                Context.RECEIVER_NOT_EXPORTED,
            )
        } else {
            context.registerReceiver(receiver, filter, broadcastPermission, null)
        }
    }

    fun capture(): JankHunterLogSnapshot? = requesterLock.withLock {
        if (closed.get()) return null
        return runCatching {
            RandomAccessFile(File(directory, EXCHANGE_LOCK_FILE), "rw").use { access ->
                access.channel.lock().use {
                    captureLock.withLock {
                        if (closed.get()) null else captureLocked()
                    }
                }
            }
        }.getOrNull()
    }

    private fun captureLocked(): JankHunterLogSnapshot? {
        removeAllResponses()
        val participants = runCatching { ProcessSnapshotParticipants.active(directory) }.getOrNull()
            ?: return null
        val self = participants.firstOrNull { participant -> participant.id == participantLease.id }
            ?: return null
        val expected = participants.mapTo(linkedSetOf(), ProcessSnapshotParticipants.Participant::id)
        if (participants.size == 1) {
            val snapshot = captureLocal() ?: return null
            return snapshot.takeIf { activeParticipantIds() == expected }
        }

        val token = BinaryLogFileHeader.randomId().toHex()
        val results = LinkedHashMap<String, SnapshotResponse>(participants.size)
        try {
            context.sendBroadcast(
                Intent(action)
                    .setPackage(context.packageName)
                    .addFlags(Intent.FLAG_RECEIVER_FOREGROUND)
                    .putExtra(EXTRA_TOKEN, token)
                    .putExtra(EXTRA_REQUESTER, self.id),
                broadcastPermission,
            )
            val local = captureLocal() ?: return null
            results[self.id] = SnapshotResponse(local.capturedAtMs, local.logPaths)
            val deadlineNs = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(CAPTURE_TIMEOUT_MS)
            while (System.nanoTime() < deadlineNs) {
                if (closed.get()) return null
                for (participantId in expected) {
                    if (participantId in results) continue
                    val response = readResponse(token, participantId) ?: continue
                    if (!response.succeeded) return null
                    results[participantId] = response
                }
                if (activeParticipantIds() != expected) return null
                if (results.keys.containsAll(expected)) return combine(results.values)
                LockSupport.parkNanos(RESPONSE_POLL_NS)
            }
            return null
        } finally {
            removeResponses(token)
        }
    }

    private fun respond(token: String, deadlineNs: Long) {
        val result = try {
            runBeforeDeadline(deadlineNs, CAPTURE_THREAD_NAME) {
                try {
                    captureBefore(deadlineNs)
                } finally {
                    responseInFlight.set(false)
                }
            }
        } catch (_: Throwable) {
            responseInFlight.set(false)
            null
        }
        writeResponse(token, participantLease.id, result?.takeIf { it.completed }?.value)
    }

    private fun captureBefore(deadlineNs: Long): JankHunterLogSnapshot? {
        val remainingNs = deadlineNs - System.nanoTime()
        if (remainingNs <= 0L) return null
        val acquired = try {
            captureLock.tryLock(remainingNs, TimeUnit.NANOSECONDS)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            false
        }
        if (!acquired) return null
        return try {
            if (closed.get() || System.nanoTime() >= deadlineNs) null else captureLocal()
        } finally {
            captureLock.unlock()
        }
    }

    private fun combine(responses: Collection<SnapshotResponse>): JankHunterLogSnapshot {
        val pathCount = responses.sumOf { response -> response.paths.size.toLong() }
        if (pathCount > MAX_PATHS) throw IOException("Too many Jank Hunter snapshot paths")
        val paths = responses.asSequence()
            .flatMap { response -> response.paths.asSequence() }
            .distinct()
            .sorted()
            .toList()
        val capturedAtMs = responses.maxOfOrNull(SnapshotResponse::capturedAtMs) ?: 0L
        val earliestAtMs = responses.minOfOrNull(SnapshotResponse::capturedAtMs) ?: capturedAtMs
        return JankHunterLogSnapshot(
            capturedAtMs = capturedAtMs,
            logPaths = paths,
            processCount = responses.size,
            captureSkewMs = (capturedAtMs - earliestAtMs).coerceAtLeast(0L),
        )
    }

    private fun activeParticipantIds(): Set<String>? =
        runCatching { ProcessSnapshotParticipants.active(directory) }
            .getOrNull()
            ?.mapTo(hashSetOf(), ProcessSnapshotParticipants.Participant::id)

    private fun writeResponse(token: String, participantId: String, snapshot: JankHunterLogSnapshot?) {
        if (!isToken(token) || !isToken(participantId)) return
        val response = responseFile(token, participantId)
        val temporary = File(directory, "${response.name}.${android.os.Process.myPid()}.tmp")
        runCatching {
            val payload = runCatching { encodeResponse(snapshot) }.getOrElse { encodeResponse(null) }
            temporary.outputStream().buffered().use { output ->
                output.write(payload)
                output.flush()
            }
            if (!temporary.renameTo(response)) throw IOException("Cannot publish ${response.name}")
        }.onFailure {
            temporary.delete()
        }
    }

    private fun readResponse(token: String, participantId: String): SnapshotResponse? {
        val file = responseFile(token, participantId)
        if (!file.isFile) return null
        return runCatching {
            RandomAccessFile(file, "r").use { access ->
                val length = access.length()
                if (length !in MIN_RESPONSE_BYTES.toLong()..MAX_RESPONSE_BYTES.toLong()) {
                    throw IOException("Invalid Jank Hunter snapshot response size")
                }
                decodeResponse(ByteArray(length.toInt()).also(access::readFully))
            }
        }.getOrNull()
    }

    private fun removeResponses(token: String) {
        directory.listFiles { file ->
            file.isFile && file.name.startsWith("$RESPONSE_PREFIX$token.") && file.name.endsWith(RESPONSE_SUFFIX)
        }.orEmpty().forEach(File::delete)
    }

    private fun removeAllResponses() {
        directory.listFiles { file ->
            file.isFile && file.name.startsWith(RESPONSE_PREFIX)
        }.orEmpty().forEach(File::delete)
    }

    private fun responseFile(token: String, participantId: String): File =
        File(directory, "$RESPONSE_PREFIX$token.$participantId$RESPONSE_SUFFIX")

    override fun close() {
        if (!closed.compareAndSet(false, true)) return
        runCatching { context.unregisterReceiver(receiver) }
        participantLease.close()
    }

    companion object {
        fun start(
            context: Context,
            directory: File,
            processName: String,
            captureLocal: () -> JankHunterLogSnapshot?,
        ): ProcessLogSnapshotCoordinator {
            val appContext = context.applicationContext ?: context
            val lease = ProcessSnapshotParticipants.join(directory, processName)
            return try {
                ProcessLogSnapshotCoordinator(appContext, directory, lease, captureLocal)
            } catch (error: Throwable) {
                lease.close()
                throw error
            }
        }

        internal fun encodeResponse(snapshot: JankHunterLogSnapshot?): ByteArray {
            val body = ByteArrayOutputStream()
            DataOutputStream(body).use { output ->
                output.write(MAGIC)
                output.writeInt(SCHEMA)
                output.writeBoolean(snapshot != null)
                output.writeLong(snapshot?.capturedAtMs ?: 0L)
                val paths = snapshot?.logPaths.orEmpty()
                require(paths.size <= MAX_PATHS) { "Too many Jank Hunter snapshot paths" }
                output.writeInt(paths.size)
                paths.forEach { path ->
                    val bytes = path.toByteArray(StandardCharsets.UTF_8)
                    require(bytes.isNotEmpty() && bytes.size <= MAX_PATH_BYTES) {
                        "Jank Hunter snapshot path must contain 1..$MAX_PATH_BYTES UTF-8 bytes"
                    }
                    require(body.size() + Int.SIZE_BYTES + bytes.size + Int.SIZE_BYTES <= MAX_RESPONSE_BYTES) {
                        "Jank Hunter snapshot response is too large"
                    }
                    output.writeInt(bytes.size)
                    output.write(bytes)
                }
            }
            val payload = body.toByteArray()
            val result = ByteArray(payload.size + Int.SIZE_BYTES)
            System.arraycopy(payload, 0, result, 0, payload.size)
            val crc = CRC32().apply { update(payload) }.value.toInt()
            result[result.lastIndex - 3] = (crc ushr 24).toByte()
            result[result.lastIndex - 2] = (crc ushr 16).toByte()
            result[result.lastIndex - 1] = (crc ushr 8).toByte()
            result[result.lastIndex] = crc.toByte()
            require(result.size <= MAX_RESPONSE_BYTES) { "Jank Hunter snapshot response is too large" }
            return result
        }

        internal fun decodeResponse(raw: ByteArray): SnapshotResponse {
            if (raw.size < MIN_RESPONSE_BYTES || raw.size > MAX_RESPONSE_BYTES) {
                throw IOException("Invalid Jank Hunter snapshot response size")
            }
            val storedCRC = ((raw[raw.lastIndex - 3].toInt() and 0xff) shl 24) or
                ((raw[raw.lastIndex - 2].toInt() and 0xff) shl 16) or
                ((raw[raw.lastIndex - 1].toInt() and 0xff) shl 8) or
                (raw[raw.lastIndex].toInt() and 0xff)
            val computedCRC = CRC32().apply { update(raw, 0, raw.size - Int.SIZE_BYTES) }.value.toInt()
            if (storedCRC != computedCRC) throw IOException("Corrupt Jank Hunter snapshot response")
            DataInputStream(ByteArrayInputStream(raw, 0, raw.size - Int.SIZE_BYTES)).use { input ->
                val magic = ByteArray(MAGIC.size).also(input::readFully)
                if (!magic.contentEquals(MAGIC) || input.readInt() != SCHEMA) {
                    throw IOException("Unsupported Jank Hunter snapshot response")
                }
                val succeeded = input.readBoolean()
                val capturedAtMs = input.readLong()
                val count = input.readInt()
                if (count !in 0..MAX_PATHS || (!succeeded && count != 0)) {
                    throw IOException("Invalid Jank Hunter snapshot path count")
                }
                val paths = ArrayList<String>(count)
                repeat(count) {
                    val length = input.readInt()
                    if (length !in 1..MAX_PATH_BYTES) throw IOException("Invalid Jank Hunter snapshot path")
                    paths += String(ByteArray(length).also(input::readFully), StandardCharsets.UTF_8)
                }
                if (input.available() != 0) throw IOException("Trailing Jank Hunter snapshot response bytes")
                return SnapshotResponse(capturedAtMs, paths, succeeded)
            }
        }

        internal fun <T> runBeforeDeadline(
            deadlineNs: Long,
            threadName: String = CAPTURE_THREAD_NAME,
            task: () -> T,
        ): DeadlineResult<T> {
            val completed = CountDownLatch(1)
            val value = AtomicReference<T?>()
            val worker = Thread(
                {
                    try {
                        value.set(runCatching(task).getOrNull())
                    } finally {
                        completed.countDown()
                    }
                },
                threadName,
            ).apply { isDaemon = true }
            worker.start()
            val remainingNs = (deadlineNs - System.nanoTime()).coerceAtLeast(0L)
            val finished = try {
                completed.await(remainingNs, TimeUnit.NANOSECONDS)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
                false
            }
            if (!finished) worker.interrupt()
            return DeadlineResult(finished, value.get().takeIf { finished })
        }

        private fun isToken(value: String): Boolean =
            value.length == TOKEN_HEX_LENGTH && value.all { char -> char in '0'..'9' || char in 'a'..'f' }

        private fun ByteArray.toHex(): String = joinToString(separator = "") { byte ->
            (byte.toInt() and 0xff).toString(16).padStart(2, '0')
        }

        private const val ACTION_SUFFIX = "JANK_HUNTER_CAPTURE_LOG_SNAPSHOT_1_0_0"
        private const val PERMISSION_SUFFIX = "permission.JANK_HUNTER_SNAPSHOT"
        private const val EXTRA_TOKEN = "io.jankhunter.snapshot.TOKEN"
        private const val EXTRA_REQUESTER = "io.jankhunter.snapshot.REQUESTER"
        private const val RESPONSE_PREFIX = ".jh-snapshot-response."
        private const val RESPONSE_SUFFIX = ".bin"
        private const val EXCHANGE_LOCK_FILE = ".jh-snapshot-exchange.lock"
        private const val RESPONSE_THREAD_NAME = "JankHunterSnapshot"
        private const val CAPTURE_THREAD_NAME = "JankHunterSnapshotCapture"
        private const val SCHEMA = 1
        private const val TOKEN_HEX_LENGTH = 32
        private const val MAX_PATHS = 16_384
        private const val MAX_PATH_BYTES = 64 * 1024
        private const val MAX_RESPONSE_BYTES = 16 * 1024 * 1024
        private const val MIN_RESPONSE_BYTES = 4 + Int.SIZE_BYTES + 1 + Long.SIZE_BYTES + Int.SIZE_BYTES + Int.SIZE_BYTES
        private const val CAPTURE_TIMEOUT_MS = 10_000L
        private const val RESPONDER_TIMEOUT_MS = 9_000L
        private val RESPONSE_POLL_NS = TimeUnit.MILLISECONDS.toNanos(10L)
        private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'S'.code.toByte(), 'R'.code.toByte())
    }

    internal data class SnapshotResponse(
        val capturedAtMs: Long,
        val paths: List<String>,
        val succeeded: Boolean = true,
    )

    internal data class DeadlineResult<T>(val completed: Boolean, val value: T?)
}
