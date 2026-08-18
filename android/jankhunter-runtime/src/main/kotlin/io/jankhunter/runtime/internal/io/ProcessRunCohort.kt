package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.channels.FileLock
import java.nio.channels.OverlappingFileLockException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicBoolean
import java.util.zip.CRC32

/**
 * Joins independently started application processes to one live application-run identity.
 *
 * Every participant holds an exclusive lease lock. The OS releases it after a process crash, so a
 * later process can remove stale leases without a heartbeat or a wall-clock timeout. Creation and
 * discovery are serialized by a directory lock across processes; therefore two concurrent first
 * processes cannot create different live run identities.
 */
internal object ProcessRunCohort {
    private val ownedLeases = ConcurrentHashMap<String, Lease>()

    fun join(directory: File): Lease {
        ensureDirectory(directory)
        return CrossProcessFileLocks.withDirectoryLock(directory, LOCK_FILE_NAME) {
            joinLocked(directory)
        }
    }

    private fun joinLocked(directory: File): Lease {
        var activeRunId: ByteArray? = null
        cohortLeaseFiles(directory).forEach { file ->
            when (leaseState(file)) {
                LeaseState.STALE -> file.delete()
                LeaseState.ACTIVE -> {
                    val runId = ownedLease(file)?.runId() ?: readRecord(file)
                    val current = activeRunId
                    if (current == null) {
                        activeRunId = runId
                    } else if (!current.contentEquals(runId)) {
                        throw IOException("Conflicting active Jank Hunter run cohorts")
                    }
                }
                LeaseState.UNKNOWN -> throw IOException("Cannot verify Jank Hunter run cohort lease ${file.name}")
            }
        }
        return createLease(directory, activeRunId ?: BinaryLogFileHeader.randomId())
    }

    private fun createLease(directory: File, runId: ByteArray): Lease {
        repeat(MAX_CREATE_ATTEMPTS) {
            val nonce = BinaryLogFileHeader.randomId().toHex()
            val finalFile = File(directory, "$LEASE_PREFIX$nonce$LEASE_SUFFIX")
            val temporary = File(directory, "${finalFile.name}.tmp")
            if (!temporary.createNewFile()) return@repeat
            val randomAccess = RandomAccessFile(temporary, "rw")
            var lock: FileLock? = null
            try {
                lock = randomAccess.channel.lock()
                writeRecord(randomAccess, runId)
                if (!temporary.renameTo(finalFile)) {
                    throw IOException("Cannot publish Jank Hunter run cohort lease ${finalFile.name}")
                }
                val lease = Lease(runId.copyOf(), directory, finalFile, randomAccess, lock)
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

    private fun writeRecord(randomAccess: RandomAccessFile, runId: ByteArray) {
        require(runId.size == RUN_ID_BYTES) { "Jank Hunter run ID must have $RUN_ID_BYTES bytes" }
        val raw = ByteArray(RECORD_BYTES)
        System.arraycopy(MAGIC, 0, raw, 0, MAGIC.size)
        ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN).putInt(MAGIC.size, SCHEMA)
        System.arraycopy(runId, 0, raw, HEADER_BYTES, runId.size)
        ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN).putInt(CRC_OFFSET, crc32(raw, CRC_OFFSET))
        randomAccess.setLength(0L)
        randomAccess.write(raw)
        randomAccess.channel.force(true)
    }

    private fun readRecord(file: File): ByteArray {
        val raw = try {
            file.readBytes()
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
        val runId = raw.copyOfRange(HEADER_BYTES, CRC_OFFSET)
        if (runId.all { it == 0.toByte() }) {
            throw IOException("Active Jank Hunter run cohort lease has a zero identity")
        }
        return runId
    }

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

    internal class Lease(
        private val identity: ByteArray,
        private val directory: File,
        private val file: File,
        private val randomAccess: RandomAccessFile,
        private val lock: FileLock,
    ) : Closeable {
        private val closed = AtomicBoolean()

        fun runId(): ByteArray = identity.copyOf()

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
    private const val SCHEMA = 1
    private const val RUN_ID_BYTES = 16
    private const val HEADER_BYTES = 8
    private const val CRC_OFFSET = HEADER_BYTES + RUN_ID_BYTES
    private const val RECORD_BYTES = CRC_OFFSET + Int.SIZE_BYTES
    private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'R'.code.toByte(), 'C'.code.toByte())
}
