package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.channels.FileLock
import java.nio.channels.OverlappingFileLockException
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean
import java.util.zip.CRC32

/**
 * Joins independently started application processes to one live application-run identity.
 *
 * Every participant holds an exclusive lease lock. The OS releases it after a process crash, so a
 * later process can remove stale leases without a heartbeat or a wall-clock timeout. Creation and
 * discovery are serialized by a directory lock across processes; therefore two concurrent first
 * processes cannot create different live run identities or reserve different daily indices.
 */
internal object ProcessRunCohort {
    private val ownedLeases = ConcurrentHashMap<String, Lease>()

    fun join(
        directory: File,
        localDate: String,
        authoritativeStoragePaths: Collection<String>? = null,
    ): Lease {
        SessionLogName.sequenceFileName(localDate)
        ensureDirectory(directory)
        return CrossProcessFileLocks.withDirectoryLock(directory, LOCK_FILE_NAME) {
            joinLocked(directory, localDate, authoritativeStoragePaths)
        }
    }

    private fun joinLocked(
        directory: File,
        localDate: String,
        authoritativeStoragePaths: Collection<String>?,
    ): Lease {
        var activeIdentity: RunIdentity? = null
        cohortLeaseFiles(directory).forEach { file ->
            when (leaseState(file)) {
                LeaseState.STALE -> file.delete()
                LeaseState.ACTIVE -> {
                    val identity = ownedLease(file)?.identity() ?: readRecord(file)
                    val current = activeIdentity
                    if (current == null) {
                        activeIdentity = identity
                    } else if (!current.matches(identity)) {
                        throw IOException("Conflicting active Jank Hunter run cohorts")
                    }
                }
                LeaseState.UNKNOWN -> throw IOException("Cannot verify Jank Hunter run cohort lease ${file.name}")
            }
        }
        activeIdentity?.let { identity -> return createLease(directory, identity) }
        val dailySessionIndex = nextDailySessionIndex(directory, localDate, authoritativeStoragePaths)
        val identity = RunIdentity(
            runId = BinaryLogFileHeader.randomId(),
            localDate = localDate,
            dailySessionIndex = dailySessionIndex,
        )
        val lease = createLease(directory, identity)
        try {
            persistNextDailySessionIndex(directory, localDate, dailySessionIndex + 1L)
            return lease
        } catch (error: Throwable) {
            runCatching { lease.close() }.exceptionOrNull()?.let(error::addSuppressed)
            throw error
        }
    }

    private fun nextDailySessionIndex(
        directory: File,
        localDate: String,
        authoritativeStoragePaths: Collection<String>?,
    ): Long {
        val sequenceFile = File(directory, SessionLogName.sequenceFileName(localDate))
        val highestStoredIndex = scanHighestDailySessionIndex(directory, localDate, authoritativeStoragePaths)
        RandomAccessFile(sequenceFile, "rw").use { sequence ->
            val completeLength = sequence.length() - sequence.length() % SEQUENCE_RECORD_BYTES
            if (completeLength != sequence.length()) sequence.setLength(completeLength)

            var position = 0L
            var persistedNextIndex: Long? = null
            val raw = ByteArray(SEQUENCE_RECORD_BYTES)
            val values = ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN)
            while (position < completeLength) {
                sequence.seek(position)
                sequence.readFully(raw)
                val nextIndex = values.getLong(0)
                val inverse = values.getLong(Long.SIZE_BYTES)
                val previous = persistedNextIndex
                if (nextIndex <= 0L || inverse != nextIndex.inv() || previous != null && nextIndex <= previous) {
                    throw IOException("Corrupt Jank Hunter daily session sequence ${sequenceFile.name}")
                }
                persistedNextIndex = nextIndex
                position += SEQUENCE_RECORD_BYTES
            }

            val scannedNextIndex = when (highestStoredIndex) {
                null -> 0L
                Long.MAX_VALUE -> throw IOException("Jank Hunter daily session index exhausted for $localDate")
                else -> highestStoredIndex + 1L
            }
            val dailySessionIndex = maxOf(persistedNextIndex ?: 0L, scannedNextIndex)
            if (dailySessionIndex == Long.MAX_VALUE) {
                throw IOException("Jank Hunter daily session index exhausted for $localDate")
            }
            return dailySessionIndex
        }
    }

    private fun scanHighestDailySessionIndex(
        directory: File,
        localDate: String,
        authoritativeStoragePaths: Collection<String>?,
    ): Long? {
        val localNames = directory.listFiles { file -> file.isFile }
            .orEmpty()
            .asSequence()
            .map(File::getName)
        val storageNames = authoritativeStoragePaths.orEmpty().asSequence().map { path -> File(path).name }
        return (localNames + storageNames)
            .mapNotNull(SessionLogName::parse)
            .filter { parsed -> parsed.localDate == localDate }
            .maxOfOrNull(SessionLogName.Parsed::dailySessionIndex)
    }

    private fun persistNextDailySessionIndex(directory: File, localDate: String, nextIndex: Long) {
        val raw = ByteBuffer.allocate(SEQUENCE_RECORD_BYTES)
            .order(ByteOrder.LITTLE_ENDIAN)
            .putLong(nextIndex)
            .putLong(nextIndex.inv())
            .array()
        RandomAccessFile(File(directory, SessionLogName.sequenceFileName(localDate)), "rw").use { sequence ->
            sequence.seek(sequence.length())
            sequence.write(raw)
            sequence.channel.force(true)
        }
    }

    private fun createLease(directory: File, identity: RunIdentity): Lease {
        repeat(MAX_CREATE_ATTEMPTS) {
            val nonce = BinaryLogFileHeader.randomId().toHex()
            val finalFile = File(directory, "$LEASE_PREFIX$nonce$LEASE_SUFFIX")
            val temporary = File(directory, "${finalFile.name}.tmp")
            if (!temporary.createNewFile()) return@repeat
            val randomAccess = RandomAccessFile(temporary, "rw")
            var lock: FileLock? = null
            try {
                lock = randomAccess.channel.lock()
                writeRecord(randomAccess, identity)
                if (!temporary.renameTo(finalFile)) {
                    throw IOException("Cannot publish Jank Hunter run cohort lease ${finalFile.name}")
                }
                val lease = Lease(identity.copy(), directory, finalFile, randomAccess, lock)
                if (ownedLeases.putIfAbsent(CrossProcessFileLocks.fileKey(finalFile), lease) != null) {
                    throw IOException("Duplicate Jank Hunter run cohort lease ${finalFile.name}")
                }
                return lease
            } catch (error: Throwable) {
                runCatching { lock?.release() }
                runCatching { randomAccess.close() }
                temporary.delete()
                finalFile.delete()
                throw error
            }
        }
        throw IOException("Cannot allocate a unique Jank Hunter run cohort lease")
    }

    private fun leaseState(file: File): LeaseState {
        if (ownedLease(file) != null) return LeaseState.ACTIVE
        return try {
            RandomAccessFile(file, "rw").use { randomAccess ->
                val lock = try {
                    randomAccess.channel.tryLock()
                } catch (_: OverlappingFileLockException) {
                    return LeaseState.ACTIVE
                }
                if (lock == null) {
                    LeaseState.ACTIVE
                } else {
                    lock.release()
                    LeaseState.STALE
                }
            }
        } catch (_: IOException) {
            LeaseState.UNKNOWN
        } catch (_: SecurityException) {
            LeaseState.UNKNOWN
        }
    }

    private fun ownedLease(file: File): Lease? =
        ownedLeases[CrossProcessFileLocks.fileKey(file)]?.takeUnless(Lease::isClosed)

    private fun writeRecord(randomAccess: RandomAccessFile, identity: RunIdentity) {
        require(identity.runId.size == RUN_ID_BYTES) { "Jank Hunter run ID must have $RUN_ID_BYTES bytes" }
        val raw = ByteArray(RECORD_BYTES)
        System.arraycopy(MAGIC, 0, raw, 0, MAGIC.size)
        val values = ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN)
        values.putInt(MAGIC.size, SCHEMA)
        System.arraycopy(identity.runId, 0, raw, RUN_ID_OFFSET, identity.runId.size)
        val date = identity.localDate.toByteArray(StandardCharsets.US_ASCII)
        require(date.size == DATE_BYTES) { "Jank Hunter run date must have $DATE_BYTES bytes" }
        System.arraycopy(date, 0, raw, DATE_OFFSET, date.size)
        values.putLong(DAILY_INDEX_OFFSET, identity.dailySessionIndex)
        values.putInt(CRC_OFFSET, crc32(raw, CRC_OFFSET))
        randomAccess.setLength(0L)
        randomAccess.write(raw)
        randomAccess.channel.force(true)
    }

    private fun readRecord(file: File): RunIdentity {
        val raw = try {
            RandomAccessFile(file, "r").use { record ->
                if (record.length() != RECORD_BYTES.toLong()) {
                    throw IOException("Invalid active Jank Hunter run cohort lease ${file.name}")
                }
                ByteArray(RECORD_BYTES).also(record::readFully)
            }
        } catch (error: Throwable) {
            throw IOException("Cannot read active Jank Hunter run cohort lease ${file.name}", error)
        }
        if (raw.size != RECORD_BYTES || !raw.copyOfRange(0, MAGIC.size).contentEquals(MAGIC)) {
            throw IOException("Invalid active Jank Hunter run cohort lease ${file.name}")
        }
        val values = ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN)
        if (values.getInt(MAGIC.size) != SCHEMA || values.getInt(CRC_OFFSET) != crc32(raw, CRC_OFFSET)) {
            throw IOException("Corrupt active Jank Hunter run cohort lease ${file.name}")
        }
        val runId = raw.copyOfRange(RUN_ID_OFFSET, RUN_ID_OFFSET + RUN_ID_BYTES)
        val localDate = String(raw, DATE_OFFSET, DATE_BYTES, StandardCharsets.US_ASCII)
        val dailySessionIndex = values.getLong(DAILY_INDEX_OFFSET)
        if (runId.all { it == 0.toByte() } || dailySessionIndex < 0L || !isValidLocalDate(localDate)) {
            throw IOException("Active Jank Hunter run cohort lease has an invalid identity")
        }
        return RunIdentity(runId, localDate, dailySessionIndex)
    }

    private fun isValidLocalDate(localDate: String): Boolean =
        runCatching { SessionLogName.sequenceFileName(localDate) }.isSuccess

    private fun crc32(raw: ByteArray, length: Int): Int {
        val crc = CRC32()
        crc.update(raw, 0, length)
        return crc.value.toInt()
    }

    private fun cohortLeaseFiles(directory: File): List<File> =
        directory.listFiles { file ->
            file.isFile && file.name.startsWith(LEASE_PREFIX) && file.name.endsWith(LEASE_SUFFIX)
        }?.toList().orEmpty()

    private fun ensureDirectory(directory: File) {
        if (directory.isDirectory) return
        if (!directory.exists() && directory.mkdirs()) return
        throw IOException("Cannot create Jank Hunter metadata directory: $directory")
    }

    private fun ByteArray.toHex(): String = joinToString(separator = "") { byte ->
        (byte.toInt() and 0xff).toString(16).padStart(2, '0')
    }

    internal class RunIdentity(
        val runId: ByteArray,
        val localDate: String,
        val dailySessionIndex: Long,
    ) {
        fun copy(): RunIdentity = RunIdentity(runId.copyOf(), localDate, dailySessionIndex)

        fun matches(other: RunIdentity): Boolean =
            dailySessionIndex == other.dailySessionIndex &&
                localDate == other.localDate &&
                runId.contentEquals(other.runId)
    }

    internal class Lease(
        identity: RunIdentity,
        private val directory: File,
        private val file: File,
        private val randomAccess: RandomAccessFile,
        private val lock: FileLock,
    ) : Closeable {
        private val identity = identity.copy()
        private val closed = AtomicBoolean()

        fun runId(): ByteArray = identity.runId.copyOf()

        fun localDate(): String = identity.localDate

        fun dailySessionIndex(): Long = identity.dailySessionIndex

        internal fun identity(): RunIdentity = identity.copy()

        internal fun isClosed(): Boolean = closed.get()

        @Synchronized
        override fun close() {
            if (closed.get()) return
            val cleanup = {
                if (closed.compareAndSet(false, true)) {
                    ownedLeases.remove(CrossProcessFileLocks.fileKey(file), this)
                    runCatching { lock.release() }
                    runCatching { randomAccess.close() }
                    runCatching { file.delete() }
                }
            }
            CrossProcessFileLocks.withDirectoryLock(directory, LOCK_FILE_NAME, cleanup)
        }
    }

    private enum class LeaseState {
        ACTIVE,
        STALE,
        UNKNOWN,
    }

    private const val LOCK_FILE_NAME = ".jh-run-cohort.lock"
    private const val LEASE_PREFIX = ".jh-run-cohort."
    private const val LEASE_SUFFIX = ".lease"
    private const val MAX_CREATE_ATTEMPTS = 1_024
    private const val SCHEMA = 2
    private const val RUN_ID_BYTES = 16
    private const val HEADER_BYTES = 8
    private const val RUN_ID_OFFSET = HEADER_BYTES
    private const val DATE_OFFSET = RUN_ID_OFFSET + RUN_ID_BYTES
    private const val DATE_BYTES = 10
    private const val DAILY_INDEX_OFFSET = DATE_OFFSET + DATE_BYTES
    private const val CRC_OFFSET = DAILY_INDEX_OFFSET + Long.SIZE_BYTES
    private const val RECORD_BYTES = CRC_OFFSET + Int.SIZE_BYTES
    private const val SEQUENCE_RECORD_BYTES = Long.SIZE_BYTES * 2
    private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'R'.code.toByte(), 'C'.code.toByte())
}
