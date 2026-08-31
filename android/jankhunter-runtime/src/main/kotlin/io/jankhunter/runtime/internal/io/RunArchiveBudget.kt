package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.RuntimeLongOperator
import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.channels.FileChannel
import java.nio.channels.FileLock
import java.nio.channels.OverlappingFileLockException
import java.util.concurrent.ConcurrentHashMap

internal class StorageBudgetExhaustedException(
    val limitBytes: Long,
    val usedBytes: Long,
    val requestedBytes: Long,
) : IOException(
    "storage_budget_exhausted: limit=$limitBytes used=$usedBytes requested=$requestedBytes",
)

/** Cross-process quota for the complete `.jhlog` archive. Claims happen before each physical write. */
internal class RunArchiveBudget private constructor(
    private val directory: File,
    private val runId: String,
    private val limitBytes: Long,
    private val stateAccess: RandomAccessFile,
    private val reservationLease: Lease,
    private val processLock: CrossProcessFileLocks.ProcessLockHandle,
    private val reclaimBytesTo: RuntimeLongOperator,
) : Closeable {
    private var remainingReservation = TERMINAL_RESERVE_BYTES
    private var closed = false
    private val stateScratch = State()
    private val stateReadBuffer = ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN)
    private val stateWriteBuffer = ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN)
    private val runIdentity = runId.hexToBytes()

    @Synchronized
    fun claim(bytes: Long, terminal: Boolean) {
        check(!closed) { "run archive budget is closed" }
        require(bytes >= 0L) { "archive byte claim must be non-negative" }
        withStateLock { channel ->
            val state = readState(channel, stateScratch)
            var nextReserved = state.reserved - if (terminal) remainingReservation else 0L
            if (!fits(state.used, nextReserved, bytes)) {
                val actualReserved = activeReservations(directory, runId).bytes
                state.reserved = actualReserved
                nextReserved = state.reserved - if (terminal) remainingReservation else 0L
                val targetBytes = (limitBytes - nextReserved - bytes).coerceAtLeast(0L)
                val reclaimedBytes = reclaimBytesTo.apply(targetBytes).coerceAtLeast(0L)
                state.used = (state.used - reclaimedBytes).coerceAtLeast(0L)
                writeState(channel, state)
            }
            if (!fits(state.used, nextReserved, bytes)) {
                throw StorageBudgetExhaustedException(limitBytes, saturatedAdd(state.used, nextReserved), bytes)
            }
            state.used += bytes
            state.reserved = nextReserved
            writeState(channel, state)
            if (terminal) {
                reservationLease.write(0L)
                remainingReservation = 0L
            }
        }
    }

    @Synchronized
    override fun close() {
        if (closed) return
        val cleanup = {
            synchronized(processLock.monitor) {
                if (closed) return@synchronized
                closed = true
                runCatching {
                    withStateLock {
                        val state = readState(it, stateScratch)
                        state.reserved = (state.reserved - remainingReservation).coerceAtLeast(0L)
                        writeState(it, state)
                        reservationLease.write(0L)
                        remainingReservation = 0L
                    }
                }
                ownedLeases.remove(reservationLease.key, reservationLease)
                reservationLease.close()
                runCatching { stateAccess.close() }
            }
        }
        CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE, cleanup)
        processLock.close()
    }

    private inline fun <T> withStateLock(block: (FileChannel) -> T): T = synchronized(processLock.monitor) {
        stateAccess.channel.lock().use { block(stateAccess.channel) }
    }

    private fun fits(used: Long, reserved: Long, requested: Long): Boolean {
        if (used < 0L || reserved < 0L || used > limitBytes || reserved > limitBytes - used) return false
        return requested <= limitBytes - used - reserved
    }

    private fun readState(channel: FileChannel, state: State): State {
        val raw = stateReadBuffer
        raw.clear()
        if (channel.size() != STATE_BYTES.toLong()) throw IOException("Invalid Jank Hunter archive budget state")
        readFully(channel, raw, 0L)
        raw.flip()
        if (!matches(raw, MAGIC)) throw IOException("Invalid Jank Hunter archive budget identity")
        val schema = raw.int
        if (schema != SCHEMA || !matches(raw, runIdentity)) {
            throw IOException("Invalid Jank Hunter archive budget identity")
        }
        val used = raw.long
        val usedInverted = raw.long
        val reserved = raw.long
        val reservedInverted = raw.long
        if (used < 0L || usedInverted != used.inv() || reserved < 0L || reservedInverted != reserved.inv()) {
            throw IOException("Corrupt Jank Hunter archive budget state")
        }
        state.used = used
        state.reserved = reserved
        return state
    }

    private fun writeState(channel: FileChannel, state: State) {
        val raw = stateWriteBuffer
        raw.clear()
        raw.put(MAGIC)
            .putInt(SCHEMA)
            .put(runIdentity)
            .putLong(state.used)
            .putLong(state.used.inv())
            .putLong(state.reserved)
            .putLong(state.reserved.inv())
        raw.flip()
        channel.truncate(0L)
        writeFully(channel, raw, 0L)
    }

    companion object {
        const val TERMINAL_RESERVE_BYTES = 8L * 1024L
        private const val SCHEMA = 1
        private const val RUN_ID_BYTES = 16
        private const val RESERVATION_BYTES = Long.SIZE_BYTES * 2
        private const val MAX_CREATE_ATTEMPTS = 1_024
        private const val STATE_FILE_PREFIX = ".jh-archive-budget."
        private const val STATE_FILE_SUFFIX = ".state"
        private const val LEASE_FILE_SUFFIX = ".lease"
        private const val DIRECTORY_LOCK_FILE = ".jh-archive-budget.lock"
        private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'A'.code.toByte(), 'B'.code.toByte())
        private val STATE_BYTES = MAGIC.size + Int.SIZE_BYTES + RUN_ID_BYTES + Long.SIZE_BYTES * 4
        private val ownedLeases = ConcurrentHashMap<String, Lease>()

        fun open(
            directory: File,
            runId: String,
            limitBytes: Long,
            actualArchiveBytes: RuntimeLongSource,
            reclaimBytesTo: RuntimeLongOperator,
        ): RunArchiveBudget {
            require(limitBytes > 0L && limitBytes < Long.MAX_VALUE) { "archive budget must be finite and positive" }
            require(isCanonicalRunId(runId)) { "archive budget run ID must be canonical" }
            if (!directory.isDirectory && !directory.mkdirs()) throw IOException("Cannot create Jank Hunter metadata directory")
            val processLock = CrossProcessFileLocks.acquireProcessLock(directory, DIRECTORY_LOCK_FILE)
            val stateFile = File(directory, "$STATE_FILE_PREFIX$runId$STATE_FILE_SUFFIX")
            return try {
                CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE) {
                    deleteObsoleteRunStates(directory, runId)
                    synchronized(processLock.monitor) {
                    RandomAccessFile(stateFile, "rw").use { stateAccess ->
                        stateAccess.channel.lock().use {
                            val active = activeReservations(directory, runId)
                            val state = if (active.count == 0) {
                                State(used = actualArchiveBytes.getAsLong().coerceAtLeast(0L), reserved = 0L)
                            } else {
                                readExistingState(stateAccess.channel, runId)
                            }
                            state.reserved = active.bytes
                            if (!fits(limitBytes, state.used, state.reserved, TERMINAL_RESERVE_BYTES)) {
                                val targetBytes = (limitBytes - state.reserved - TERMINAL_RESERVE_BYTES)
                                    .coerceAtLeast(0L)
                                val reclaimedBytes = reclaimBytesTo.apply(targetBytes).coerceAtLeast(0L)
                                state.used = (state.used - reclaimedBytes).coerceAtLeast(0L)
                            }
                            if (
                                state.used > limitBytes ||
                                state.reserved > limitBytes - state.used ||
                                TERMINAL_RESERVE_BYTES > limitBytes - state.used - state.reserved
                            ) {
                                throw StorageBudgetExhaustedException(
                                    limitBytes,
                                    saturatedAdd(state.used, state.reserved),
                                    TERMINAL_RESERVE_BYTES,
                                )
                            }
                            val lease = createLease(directory, runId)
                            try {
                                state.reserved += TERMINAL_RESERVE_BYTES
                                writeState(stateAccess.channel, runId, state)
                                RunArchiveBudget(
                                    directory,
                                    runId,
                                    limitBytes,
                                    RandomAccessFile(stateFile, "rw"),
                                    lease,
                                    processLock,
                                    reclaimBytesTo,
                                )
                            } catch (error: Throwable) {
                                ownedLeases.remove(lease.key, lease)
                                lease.close()
                                throw error
                            }
                        }
                    }
                    }
                }
            } catch (error: Throwable) {
                processLock.close()
                throw error
            }
        }

        fun retainedJhlogBytes(paths: Iterable<String>): Long {
            var total = 0L
            paths.forEach { path ->
                val file = File(path)
                if (SessionLogName.parse(file.name) != null) {
                    total = saturatedAdd(total, file.length().coerceAtLeast(0L))
                }
            }
            return total
        }

        private fun fits(limitBytes: Long, used: Long, reserved: Long, requested: Long): Boolean {
            if (used < 0L || reserved < 0L || used > limitBytes || reserved > limitBytes - used) return false
            return requested <= limitBytes - used - reserved
        }

        private fun createLease(directory: File, runId: String): Lease {
            repeat(MAX_CREATE_ATTEMPTS) {
                val nonce = SessionLogName.runIdHex(BinaryLogFileHeader.randomId())
                val file = File(directory, "$STATE_FILE_PREFIX$runId.$nonce$LEASE_FILE_SUFFIX")
                if (!file.createNewFile()) return@repeat
                val access = RandomAccessFile(file, "rw")
                var lock: FileLock? = null
                try {
                    lock = access.channel.lock()
                    writeReservation(access.channel, TERMINAL_RESERVE_BYTES)
                    val lease = Lease(file, access, lock)
                    if (ownedLeases.putIfAbsent(lease.key, lease) != null) {
                        throw IOException("Duplicate Jank Hunter archive budget lease ${file.name}")
                    }
                    return lease
                } catch (error: Throwable) {
                    runCatching { lock?.release() }
                    runCatching { access.close() }
                    file.delete()
                    throw error
                }
            }
            throw IOException("Cannot allocate Jank Hunter archive budget lease")
        }

        private fun activeReservations(directory: File, runId: String): ReservationSnapshot {
            var total = 0L
            var count = 0
            directory.listFiles { file ->
                file.isFile &&
                    file.name.startsWith("$STATE_FILE_PREFIX$runId.") &&
                    file.name.endsWith(LEASE_FILE_SUFFIX)
            }.orEmpty().forEach { file ->
                val owned = ownedLease(file)
                when (if (owned == null) leaseState(file) else LeaseState.ACTIVE) {
                    LeaseState.ACTIVE -> {
                        count++
                        total = saturatedAdd(total, owned?.read() ?: readReservation(file))
                    }
                    LeaseState.STALE -> file.delete()
                    LeaseState.UNKNOWN -> throw IOException("Cannot verify Jank Hunter archive budget lease ${file.name}")
                }
            }
            return ReservationSnapshot(count, total)
        }

        private fun deleteObsoleteRunStates(directory: File, currentRunId: String) {
            val files = try {
                directory.listFiles()
            } catch (_: SecurityException) {
                null
            } ?: return
            files.forEach { file ->
                val obsoleteRunId = stateRunId(file.name) ?: return@forEach
                if (obsoleteRunId == currentRunId || !file.isFile) return@forEach
                try {
                    RandomAccessFile(file, "rw").use { stateAccess ->
                        stateAccess.channel.lock().use {
                            if (activeReservations(directory, obsoleteRunId).count == 0) {
                                file.delete()
                            }
                        }
                    }
                } catch (_: IOException) {
                    // An unverifiable or concurrently active state is preserved fail-closed.
                } catch (_: SecurityException) {
                    // An unverifiable or concurrently active state is preserved fail-closed.
                }
            }
        }

        private fun stateRunId(name: String): String? {
            val identityStart = STATE_FILE_PREFIX.length
            val identityEnd = identityStart + RUN_ID_BYTES * 2
            if (
                name.length != identityEnd + STATE_FILE_SUFFIX.length ||
                !name.startsWith(STATE_FILE_PREFIX) ||
                !name.endsWith(STATE_FILE_SUFFIX)
            ) {
                return null
            }
            var nonZero = false
            for (index in identityStart until identityEnd) {
                val character = name[index]
                if (character !in '0'..'9' && character !in 'a'..'f') return null
                if (character != '0') nonZero = true
            }
            return if (nonZero) name.substring(identityStart, identityEnd) else null
        }

        private fun isCanonicalRunId(runId: String): Boolean {
            if (runId.length != RUN_ID_BYTES * 2) return false
            var nonZero = false
            runId.forEach { character ->
                if (character !in '0'..'9' && character !in 'a'..'f') return false
                if (character != '0') nonZero = true
            }
            return nonZero
        }

        private fun leaseState(file: File): LeaseState = try {
            RandomAccessFile(file, "rw").use { access ->
                val lock = try {
                    access.channel.tryLock()
                } catch (_: OverlappingFileLockException) {
                    return LeaseState.ACTIVE
                }
                if (lock == null) LeaseState.ACTIVE else {
                    lock.release()
                    LeaseState.STALE
                }
            }
        } catch (_: IOException) {
            LeaseState.UNKNOWN
        } catch (_: SecurityException) {
            LeaseState.UNKNOWN
        }

        private fun ownedLease(file: File): Lease? =
            ownedLeases[CrossProcessFileLocks.fileKey(file)]?.takeUnless(Lease::isClosed)

        private fun readReservation(file: File): Long = RandomAccessFile(file, "r").use { access ->
            readReservation(access.channel)
        }

        private fun readReservation(channel: FileChannel): Long {
            val raw = ByteBuffer.allocate(RESERVATION_BYTES).order(ByteOrder.LITTLE_ENDIAN)
            if (channel.size() != RESERVATION_BYTES.toLong()) {
                throw IOException("Invalid Jank Hunter archive reservation")
            }
            readFully(channel, raw, 0L)
            raw.flip()
            val bytes = raw.long
            val inverted = raw.long
            if (bytes < 0L || inverted != bytes.inv()) throw IOException("Corrupt Jank Hunter archive reservation")
            return bytes
        }

        private fun writeReservation(channel: FileChannel, bytes: Long) {
            val raw = ByteBuffer.allocate(RESERVATION_BYTES).order(ByteOrder.LITTLE_ENDIAN)
                .putLong(bytes)
                .putLong(bytes.inv())
            raw.flip()
            channel.truncate(0L)
            writeFully(channel, raw, 0L)
        }

        private fun readExistingState(channel: FileChannel, runId: String): State {
            val raw = ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN)
            if (channel.size() != STATE_BYTES.toLong()) throw IOException("Invalid Jank Hunter archive budget state")
            readFully(channel, raw, 0L)
            raw.flip()
            val magic = ByteArray(MAGIC.size).also(raw::get)
            val schema = raw.int
            val identity = ByteArray(RUN_ID_BYTES).also(raw::get)
            val used = raw.long
            val usedInverted = raw.long
            val reserved = raw.long
            val reservedInverted = raw.long
            if (!magic.contentEquals(MAGIC) || schema != SCHEMA || SessionLogName.runIdHex(identity) != runId) {
                throw IOException("Invalid Jank Hunter archive budget identity")
            }
            if (used < 0L || usedInverted != used.inv() || reserved < 0L || reservedInverted != reserved.inv()) {
                throw IOException("Corrupt Jank Hunter archive budget state")
            }
            return State(used, reserved)
        }

        private fun writeState(channel: FileChannel, runId: String, state: State) {
            val raw = ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN)
                .put(MAGIC)
                .putInt(SCHEMA)
                .put(runId.hexToBytes())
                .putLong(state.used)
                .putLong(state.used.inv())
                .putLong(state.reserved)
                .putLong(state.reserved.inv())
            raw.flip()
            channel.truncate(0L)
            writeFully(channel, raw, 0L)
        }

        private fun String.hexToBytes(): ByteArray {
            val bytes = ByteArray(length / 2)
            for (index in bytes.indices) {
                val high = Character.digit(this[index * 2], 16)
                val low = Character.digit(this[index * 2 + 1], 16)
                if (high < 0 || low < 0) throw IOException("Invalid Jank Hunter archive budget run ID")
                bytes[index] = ((high shl 4) or low).toByte()
            }
            return bytes
        }

        private fun readFully(channel: FileChannel, target: ByteBuffer, position: Long) {
            var offset = position
            while (target.hasRemaining()) {
                val read = channel.read(target, offset)
                if (read <= 0) throw IOException("Cannot read Jank Hunter archive budget state")
                offset += read
            }
        }

        private fun writeFully(channel: FileChannel, source: ByteBuffer, position: Long) {
            var offset = position
            while (source.hasRemaining()) {
                val written = channel.write(source, offset)
                if (written <= 0) throw IOException("Cannot write Jank Hunter archive budget state")
                offset += written
            }
        }

        private fun matches(source: ByteBuffer, expected: ByteArray): Boolean {
            expected.forEach { byte ->
                if (!source.hasRemaining() || source.get() != byte) return false
            }
            return true
        }

        private class Lease(
            private val file: File,
            private val access: RandomAccessFile,
            private val lock: FileLock,
        ) : Closeable {
            val key: String = CrossProcessFileLocks.fileKey(file)
            private var closed = false

            @Synchronized
            fun read(): Long {
                check(!closed) { "archive budget lease is closed" }
                return readReservation(access.channel)
            }

            @Synchronized
            fun write(bytes: Long) {
                check(!closed) { "archive budget lease is closed" }
                writeReservation(access.channel, bytes)
            }

            @Synchronized
            fun isClosed(): Boolean = closed

            @Synchronized
            override fun close() {
                if (closed) return
                closed = true
                runCatching { lock.release() }
                runCatching { access.close() }
                file.delete()
            }
        }

        private data class ReservationSnapshot(val count: Int, val bytes: Long)

        private enum class LeaseState { ACTIVE, STALE, UNKNOWN }
    }

    private class State(var used: Long = 0L, var reserved: Long = 0L)
}
