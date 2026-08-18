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

/** Crash-safe registry of runtime processes that can participate in a coordinated log snapshot. */
internal object ProcessSnapshotParticipants {
    private val ownedLeases = ConcurrentHashMap<String, Lease>()

    fun join(directory: File, processName: String): Lease {
        val nameBytes = processName.toByteArray(StandardCharsets.UTF_8)
        require(nameBytes.isNotEmpty() && nameBytes.size <= MAX_PROCESS_NAME_BYTES) {
            "Jank Hunter snapshot process name must contain 1..$MAX_PROCESS_NAME_BYTES UTF-8 bytes"
        }
        ensureDirectory(directory)
        return CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE) {
            removeStaleTemporaryFiles(directory)
            removeStale(directory)
            createLease(directory, nameBytes)
        }
    }

    fun active(directory: File): List<Participant> =
        CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE) {
            activeLocked(directory)
        }

    private fun activeLocked(directory: File): List<Participant> {
        val participants = ArrayList<Participant>()
        participantFiles(directory).forEach { file ->
            when (leaseState(file)) {
                LeaseState.STALE -> file.delete()
                LeaseState.ACTIVE -> participants += readParticipant(file)
                LeaseState.UNKNOWN -> throw IOException("Cannot verify Jank Hunter snapshot participant ${file.name}")
            }
        }
        return participants.sortedBy(Participant::id)
    }

    private fun createLease(directory: File, processName: ByteArray): Lease {
        repeat(MAX_CREATE_ATTEMPTS) {
            val id = BinaryLogFileHeader.randomId().toHex()
            val finalFile = File(directory, "$LEASE_PREFIX$id$LEASE_SUFFIX")
            val temporary = File(directory, "${finalFile.name}.tmp")
            if (!temporary.createNewFile()) return@repeat
            val access = RandomAccessFile(temporary, "rw")
            var lock: FileLock? = null
            try {
                lock = access.channel.lock()
                writeRecord(access, processName)
                if (!temporary.renameTo(finalFile)) {
                    throw IOException("Cannot publish Jank Hunter snapshot participant ${finalFile.name}")
                }
                val participant = Participant(id, String(processName, StandardCharsets.UTF_8))
                val lease = Lease(id, directory, finalFile, access, lock, participant)
                if (ownedLeases.putIfAbsent(CrossProcessFileLocks.fileKey(finalFile), lease) != null) {
                    throw IOException("Duplicate Jank Hunter snapshot participant ${finalFile.name}")
                }
                return lease
            } catch (error: Throwable) {
                runCatching { lock?.release() }
                runCatching { access.close() }
                temporary.delete()
                finalFile.delete()
                throw error
            }
        }
        throw IOException("Cannot allocate a unique Jank Hunter snapshot participant")
    }

    private fun writeRecord(access: RandomAccessFile, processName: ByteArray) {
        val raw = ByteArray(HEADER_BYTES + processName.size + Int.SIZE_BYTES)
        System.arraycopy(MAGIC, 0, raw, 0, MAGIC.size)
        val values = ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN)
        values.putInt(MAGIC.size, SCHEMA)
        values.putInt(MAGIC.size + Int.SIZE_BYTES, processName.size)
        System.arraycopy(processName, 0, raw, HEADER_BYTES, processName.size)
        values.putInt(raw.size - Int.SIZE_BYTES, crc32(raw, raw.size - Int.SIZE_BYTES))
        access.setLength(0L)
        access.write(raw)
        access.channel.force(true)
    }

    private fun readParticipant(file: File): Participant {
        ownedLease(file)?.let { return it.participant }
        val id = participantId(file.name)
            ?: throw IOException("Invalid Jank Hunter snapshot participant name ${file.name}")
        val raw = try {
            RandomAccessFile(file, "r").use { access ->
                val length = access.length()
                if (length !in MIN_RECORD_BYTES.toLong()..MAX_RECORD_BYTES.toLong()) {
                    throw IOException("Invalid Jank Hunter snapshot participant size")
                }
                ByteArray(length.toInt()).also(access::readFully)
            }
        } catch (error: Throwable) {
            throw IOException("Cannot read Jank Hunter snapshot participant ${file.name}", error)
        }
        if (raw.size < HEADER_BYTES + Int.SIZE_BYTES ||
            !raw.copyOfRange(0, MAGIC.size).contentEquals(MAGIC)
        ) {
            throw IOException("Invalid Jank Hunter snapshot participant ${file.name}")
        }
        val values = ByteBuffer.wrap(raw).order(ByteOrder.LITTLE_ENDIAN)
        val processNameSize = values.getInt(MAGIC.size + Int.SIZE_BYTES)
        if (values.getInt(MAGIC.size) != SCHEMA ||
            processNameSize !in 1..MAX_PROCESS_NAME_BYTES ||
            raw.size != HEADER_BYTES + processNameSize + Int.SIZE_BYTES ||
            values.getInt(raw.size - Int.SIZE_BYTES) != crc32(raw, raw.size - Int.SIZE_BYTES)
        ) {
            throw IOException("Corrupt Jank Hunter snapshot participant ${file.name}")
        }
        val processName = String(raw, HEADER_BYTES, processNameSize, StandardCharsets.UTF_8)
        return Participant(id, processName)
    }

    private fun removeStale(directory: File) {
        participantFiles(directory).forEach { file ->
            when (leaseState(file)) {
                LeaseState.STALE -> file.delete()
                LeaseState.ACTIVE -> Unit
                LeaseState.UNKNOWN -> throw IOException("Cannot verify Jank Hunter snapshot participant ${file.name}")
            }
        }
    }

    private fun removeStaleTemporaryFiles(directory: File) {
        directory.listFiles { file ->
            file.isFile && file.name.startsWith(LEASE_PREFIX) && file.name.endsWith(TEMP_SUFFIX)
        }.orEmpty().forEach { file ->
            when (leaseState(file)) {
                LeaseState.STALE -> file.delete()
                LeaseState.ACTIVE -> Unit
                LeaseState.UNKNOWN -> throw IOException(
                    "Cannot verify Jank Hunter snapshot temporary participant ${file.name}",
                )
            }
        }
    }

    private fun leaseState(file: File): LeaseState {
        if (ownedLease(file) != null) return LeaseState.ACTIVE
        return try {
            RandomAccessFile(file, "rw").use { access ->
                val lock = try {
                    access.channel.tryLock()
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

    private fun participantFiles(directory: File): List<File> = directory.listFiles { file ->
        file.isFile && participantId(file.name) != null
    }.orEmpty().toList()

    private fun participantId(name: String): String? {
        if (!name.startsWith(LEASE_PREFIX) || !name.endsWith(LEASE_SUFFIX)) return null
        val id = name.removePrefix(LEASE_PREFIX).removeSuffix(LEASE_SUFFIX)
        return id.takeIf { value -> value.length == ID_HEX_LENGTH && value.all(::isLowerHex) }
    }

    private fun ensureDirectory(directory: File) {
        if (directory.isDirectory) return
        if (!directory.exists() && directory.mkdirs()) return
        throw IOException("Cannot create Jank Hunter metadata directory: $directory")
    }

    private fun crc32(raw: ByteArray, length: Int): Int {
        val crc = CRC32()
        crc.update(raw, 0, length)
        return crc.value.toInt()
    }

    private fun isLowerHex(value: Char): Boolean = value in '0'..'9' || value in 'a'..'f'

    private fun ByteArray.toHex(): String = joinToString(separator = "") { byte ->
        (byte.toInt() and 0xff).toString(16).padStart(2, '0')
    }

    data class Participant(val id: String, val processName: String)

    class Lease internal constructor(
        val id: String,
        private val directory: File,
        private val file: File,
        private val access: RandomAccessFile,
        private val lock: FileLock,
        internal val participant: Participant,
    ) : Closeable {
        private val closed = AtomicBoolean()

        internal fun isClosed(): Boolean = closed.get()

        @Synchronized
        override fun close() {
            if (closed.get()) return
            val cleanup = {
                if (closed.compareAndSet(false, true)) {
                    ownedLeases.remove(CrossProcessFileLocks.fileKey(file), this)
                    runCatching { lock.release() }
                    runCatching { access.close() }
                    runCatching { file.delete() }
                }
            }
            CrossProcessFileLocks.withDirectoryLock(directory, DIRECTORY_LOCK_FILE, cleanup)
        }
    }

    private enum class LeaseState {
        ACTIVE,
        STALE,
        UNKNOWN,
    }

    private const val LEASE_PREFIX = ".jh-snapshot-participant."
    private const val DIRECTORY_LOCK_FILE = ".jh-snapshot-participants.lock"
    private const val LEASE_SUFFIX = ".lease"
    private const val TEMP_SUFFIX = "$LEASE_SUFFIX.tmp"
    private const val MAX_PROCESS_NAME_BYTES = 4 * 1024
    private const val MAX_CREATE_ATTEMPTS = 1_024
    private const val ID_HEX_LENGTH = 32
    private const val SCHEMA = 1
    private const val HEADER_BYTES = 12
    private const val MIN_RECORD_BYTES = HEADER_BYTES + 1 + Int.SIZE_BYTES
    private const val MAX_RECORD_BYTES = HEADER_BYTES + MAX_PROCESS_NAME_BYTES + Int.SIZE_BYTES
    private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'S'.code.toByte(), 'P'.code.toByte())
}
