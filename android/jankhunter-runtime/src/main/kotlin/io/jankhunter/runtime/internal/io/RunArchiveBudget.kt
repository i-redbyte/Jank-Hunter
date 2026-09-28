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
    private val sharedLimit: Boolean,
    initialReservationBytes: Long,
) : Closeable {
    private var remainingReservation = initialReservationBytes
    private var closed = false
    private val stateScratch = State()
    private val alternateScratch = State()
    private val stateReadBuffer = ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN)
    private val stateWriteBuffer = ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN)
    private val runIdentity = runId.hexToBytes()

    @Synchronized
    fun claim(bytes: Long, terminal: Boolean) {
        check(!closed) { "run archive budget is closed" }
        require(bytes >= 0L) { "archive byte claim must be non-negative" }
        withStateLock { channel ->
            val state = readState(channel, stateScratch)
            val activeLimit = if (sharedLimit) state.limit else limitBytes
            var nextReserved = state.reserved - if (terminal) remainingReservation else 0L
            if (!fits(activeLimit, state.used, nextReserved, bytes)) {
                val actualReserved = activeReservations(directory, runId).bytes
                state.reserved = actualReserved
                nextReserved = state.reserved - if (terminal) remainingReservation else 0L
                val targetBytes = (activeLimit - nextReserved - bytes).coerceAtLeast(0L)
                val reclaimedBytes = reclaimBytesTo.apply(targetBytes).coerceAtLeast(0L)
                state.used = (state.used - reclaimedBytes).coerceAtLeast(0L)
                writeState(channel, state)
            }
            if (!fits(activeLimit, state.used, nextReserved, bytes)) {
                throw StorageBudgetExhaustedException(activeLimit, saturatedAdd(state.used, nextReserved), bytes)
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
    fun reclaimTo(targetBytes: Long): Long {
        require(targetBytes >= 0L)
        check(!closed)
        return withStateLock { channel ->
            val state = readState(channel, stateScratch)
            val reclaimed = reclaimBytesTo.apply(targetBytes).coerceAtLeast(0L)
            state.used = (state.used - reclaimed).coerceAtLeast(0L)
            writeState(channel, state)
            reclaimed
        }
    }

    /** Releases an unpublished external artifact claim; writer claims stay conservative on I/O failure. */
    @Synchronized
    fun releaseClaim(bytes: Long) {
        require(bytes >= 0L)
        check(!closed)
        withStateLock { channel ->
            val state = readState(channel, stateScratch)
            state.used = (state.used - bytes).coerceAtLeast(0L)
            writeState(channel, state)
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

    private fun fits(activeLimit: Long, used: Long, reserved: Long, requested: Long): Boolean {
        if (used < 0L || reserved < 0L || used > activeLimit || reserved > activeLimit - used) return false
        return requested <= activeLimit - used - reserved
    }

    private fun readState(channel: FileChannel, state: State): State {
        return readState(channel, runIdentity, state, alternateScratch, stateReadBuffer)
    }

    private fun writeState(channel: FileChannel, state: State) {
        writeState(channel, runIdentity, state, stateWriteBuffer)
    }

    companion object {
        const val TERMINAL_RESERVE_BYTES = 8L * 1024L
        private const val SCHEMA = 3
        private const val RUN_ID_BYTES = 16
        private const val RESERVATION_BYTES = Long.SIZE_BYTES * 2
        private const val MAX_CREATE_ATTEMPTS = 1_024
        private const val STATE_FILE_PREFIX = ".jh-archive-budget."
        private const val STATE_FILE_SUFFIX = ".state"
        private const val LEASE_FILE_SUFFIX = ".lease"
        private const val DIRECTORY_LOCK_FILE = ".jh-archive-budget.lock"
        private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'A'.code.toByte(), 'B'.code.toByte())
        private val STATE_BYTES = MAGIC.size + Int.SIZE_BYTES + RUN_ID_BYTES + Long.SIZE_BYTES * 8
        private val ownedLeases = ConcurrentHashMap<String, Lease>()

        fun open(
            directory: File,
            runId: String,
            limitBytes: Long,
            actualArchiveBytes: RuntimeLongSource,
            sharedLimit: Boolean = false,
            reservationBytes: Long = TERMINAL_RESERVE_BYTES,
            reconcileOnOpen: Boolean = false,
            reclaimBytesTo: RuntimeLongOperator,
        ): RunArchiveBudget {
            require(reservationBytes >= 0L)
            require(limitBytes > 0L) { "archive budget must be positive" }
            require(isCanonicalRunId(runId)) { "archive budget run ID must be canonical" }
            if (!directory.isDirectory && !directory.mkdirs() && !directory.isDirectory) {
                throw IOException("Cannot create Jank Hunter metadata directory")
            }
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
                                State(used = actualArchiveBytes.getAsLong().coerceAtLeast(0L), reserved = 0L, limit = limitBytes)
                            } else {
                                readExistingState(stateAccess.channel, runId)
                            }
                            state.reserved = active.bytes
                            if (sharedLimit && active.count > 0) state.limit = minOf(state.limit, limitBytes)
                            if (reconcileOnOpen && active.count > 0) {
                                // Claims can precede their flush, so a live ledger must never be reduced by a scan.
                                state.used = maxOf(state.used, actualArchiveBytes.getAsLong())
                            }
                            val effectiveLimit = if (sharedLimit) state.limit else limitBytes
                            if (!fits(effectiveLimit, state.used, state.reserved, reservationBytes)) {
                                val targetBytes = (effectiveLimit - state.reserved - reservationBytes)
                                    .coerceAtLeast(0L)
                                val reclaimedBytes = reclaimBytesTo.apply(targetBytes).coerceAtLeast(0L)
                                state.used = (state.used - reclaimedBytes).coerceAtLeast(0L)
                            }
                            if (sharedLimit) writeState(stateAccess.channel, runId, state)
                            if (
                                state.used > effectiveLimit ||
                                state.reserved > effectiveLimit - state.used ||
                                reservationBytes > effectiveLimit - state.used - state.reserved
                            ) {
                                throw StorageBudgetExhaustedException(
                                    effectiveLimit,
                                    saturatedAdd(state.used, state.reserved),
                                    reservationBytes,
                                )
                            }
                            val lease = createLease(directory, runId, reservationBytes)
                            try {
                                state.reserved += reservationBytes
                                writeState(stateAccess.channel, runId, state)
                                RunArchiveBudget(
                                    directory,
                                    runId,
                                    limitBytes,
                                    RandomAccessFile(stateFile, "rw"),
                                    lease,
                                    processLock,
                                    reclaimBytesTo,
                                    sharedLimit,
                                    reservationBytes,
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

        /** Cold publication transaction. Payload staging happens before this short metadata lock. */
        fun mutateAccountedFiles(
            directory: File,
            runId: String,
            limitBytes: Long,
            actualBytes: () -> Long,
            requestedBytes: Long,
            trackedBytes: () -> Long,
            mutation: () -> Boolean,
        ): Boolean {
            require(requestedBytes >= 0L)
            if (!directory.isDirectory && !directory.mkdirs() && !directory.isDirectory) {
                throw IOException("Cannot create Jank Hunter metadata directory")
            }
            return CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE) {
                RandomAccessFile(File(directory, "$STATE_FILE_PREFIX$runId$STATE_FILE_SUFFIX"), "rw").use { access ->
                    access.channel.lock().use {
                        val active = activeReservations(directory, runId)
                        val state = if (active.count == 0) State(actualBytes(), 0L, limitBytes)
                            else readExistingState(access.channel, runId)
                        state.reserved = active.bytes
                        if (active.count > 0) state.limit = minOf(state.limit, limitBytes)
                        val effectiveLimit = state.limit
                        if (requestedBytes > 0L && !fits(effectiveLimit, state.used, state.reserved, requestedBytes)) {
                            writeState(access.channel, runId, state)
                            return@withDirectoryLock false
                        }
                        val before = trackedBytes()
                        state.used = saturatedAdd(state.used, requestedBytes)
                        writeState(access.channel, runId, state)
                        try {
                            mutation()
                        } finally {
                            val after = trackedBytes()
                            state.used = (state.used - requestedBytes - before).coerceAtLeast(0L)
                            state.used = saturatedAdd(state.used, after)
                            writeState(access.channel, runId, state)
                        }
                    }
                }
            }
        }

        /** Reclaims an immutable artifact without admitting a new writer or reserving quota. */
        fun deleteAccountedFile(directory: File, runId: String, file: File, delete: () -> Boolean): Boolean {
            if (!directory.isDirectory && !directory.mkdirs() && !directory.isDirectory) {
                throw IOException("Cannot create Jank Hunter metadata directory")
            }
            return CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE) {
                val stateFile = File(directory, "$STATE_FILE_PREFIX$runId$STATE_FILE_SUFFIX")
                if (!stateFile.isFile) return@withDirectoryLock delete()
                RandomAccessFile(stateFile, "rw").use { access ->
                    access.channel.lock().use {
                        val state = readExistingState(access.channel, runId)
                        val bytes = file.length().coerceAtLeast(0L)
                        val result = delete()
                        // A live export can defer deletion while reporting retention as successful.
                        if (!file.exists()) {
                            state.used = (state.used - bytes).coerceAtLeast(0L)
                            writeState(access.channel, runId, state)
                        }
                        result
                    }
                }
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

        private fun createLease(directory: File, runId: String, reservationBytes: Long): Lease {
            repeat(MAX_CREATE_ATTEMPTS) {
                val nonce = SessionLogName.runIdHex(BinaryLogFileHeader.randomId())
                val file = File(directory, "$STATE_FILE_PREFIX$runId.$nonce$LEASE_FILE_SUFFIX")
                if (!file.createNewFile()) return@repeat
                val access = RandomAccessFile(file, "rw")
                var lock: FileLock? = null
                try {
                    lock = access.channel.lock()
                    writeReservation(access.channel, reservationBytes)
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
            return readState(
                channel, runId.hexToBytes(), State(), State(),
                ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN),
            )
        }

        private fun writeState(channel: FileChannel, runId: String, state: State) {
            writeState(channel, runId.hexToBytes(), state, ByteBuffer.allocate(STATE_BYTES).order(ByteOrder.LITTLE_ENDIAN))
        }

        private fun readState(
            channel: FileChannel, identity: ByteArray, state: State, alternate: State, raw: ByteBuffer,
        ): State {
            val size = channel.size()
            if (size != STATE_BYTES.toLong() && size != STATE_BYTES * 2L) {
                throw IOException("Invalid Jank Hunter archive budget state")
            }
            val first = readSlot(channel, identity, state, raw, 0)
            val second = if (size == STATE_BYTES * 2L) readSlot(channel, identity, alternate, raw, 1) else false
            // With active leases, the previous record cannot include an unflushed claim. A torn
            // slot must stop admission; the next lease-free open rebuilds from physical artifacts.
            if (!first || (size == STATE_BYTES * 2L && !second)) {
                throw IOException("Corrupt Jank Hunter archive budget state")
            }
            if (second && alternate.generation > state.generation) state.copyFrom(alternate)
            return state
        }

        private fun readSlot(
            channel: FileChannel, identity: ByteArray, state: State, raw: ByteBuffer, slot: Int,
        ): Boolean {
            raw.clear()
            readFully(channel, raw, slot.toLong() * STATE_BYTES)
            raw.flip()
            if (!matches(raw, MAGIC) || raw.int != SCHEMA || !matches(raw, identity)) return false
            val generation = raw.long
            val generationInverse = raw.long
            val used = raw.long
            val usedInverse = raw.long
            val reserved = raw.long
            val reservedInverse = raw.long
            val limit = raw.long
            val limitInverse = raw.long
            if (generation <= 0L || generationInverse != generation.inv() || used < 0L || usedInverse != used.inv() ||
                reserved < 0L || reservedInverse != reserved.inv() || limit <= 0L || limitInverse != limit.inv()
            ) return false
            state.used = used
            state.reserved = reserved
            state.limit = limit
            state.generation = generation
            state.slot = slot
            return true
        }

        private fun writeState(channel: FileChannel, identity: ByteArray, state: State, raw: ByteBuffer) {
            val nextSlot = if (state.slot == 0) 1 else 0
            val nextGeneration = if (state.generation == Long.MAX_VALUE) 1L else state.generation + 1L
            raw.clear()
            raw.put(MAGIC).putInt(SCHEMA).put(identity)
                .putLong(nextGeneration).putLong(nextGeneration.inv())
                .putLong(state.used).putLong(state.used.inv())
                .putLong(state.reserved).putLong(state.reserved.inv())
                .putLong(state.limit).putLong(state.limit.inv())
            raw.flip()
            if (state.slot < 0) channel.truncate(0L)
            writeFully(channel, raw, nextSlot.toLong() * STATE_BYTES)
            channel.force(false)
            state.slot = nextSlot
            state.generation = nextGeneration
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

    private class State(
        var used: Long = 0L,
        var reserved: Long = 0L,
        var limit: Long = Long.MAX_VALUE,
        var generation: Long = 0L,
        var slot: Int = -1,
    ) {
        fun copyFrom(other: State) {
            used = other.used
            reserved = other.reserved
            limit = other.limit
            generation = other.generation
            slot = other.slot
        }
    }
}
