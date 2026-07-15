package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.channels.FileChannel
import java.nio.channels.FileLock
import java.nio.channels.OverlappingFileLockException
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.atomic.AtomicLong

internal object SessionLogName {
    const val PREFIX = "jh-session-log."
    const val SUFFIX = ".jhlog"

    fun create(localDate: String, index: Long): String {
        require(LOCAL_DATE.matches(localDate)) { "session log date must use yyyy-MM-dd" }
        require(index >= 0L) { "session log index must be non-negative" }
        return "$PREFIX$localDate.$index$SUFFIX"
    }

    fun parse(fileName: String): Parsed? {
        if (!fileName.startsWith(PREFIX) || !fileName.endsWith(SUFFIX)) return null
        val body = fileName.removePrefix(PREFIX).removeSuffix(SUFFIX)
        val separator = body.lastIndexOf('.')
        if (separator <= 0 || separator == body.lastIndex) return null
        val localDate = body.substring(0, separator)
        if (!LOCAL_DATE.matches(localDate)) return null
        val index = body.substring(separator + 1).toLongOrNull()?.takeIf { it >= 0L } ?: return null
        return Parsed(localDate, index)
    }

    data class Parsed(
        val localDate: String,
        val index: Long,
    )

    private val LOCAL_DATE = Regex("\\d{4}-\\d{2}-\\d{2}")
}

internal object SessionLogAllocator {
    private const val SEQUENCE_RECORD_BYTES = Long.SIZE_BYTES * 2
    private const val MAX_LEASE_PATH_BYTES = 64 * 1024
    private val directoryLocks = ConcurrentHashMap<String, Any>()
    private val temporaryLeaseId = AtomicLong()

    fun reserve(
        directory: File,
        localDate: String,
        authoritativeStoragePaths: Collection<String>? = null,
        minimumIndex: Long = 0L,
    ): Allocation {
        SessionLogName.create(localDate, 0L)
        require(minimumIndex >= 0L) { "minimum session log index must be non-negative" }
        ensureDirectory(directory)
        val directoryKey = runCatching { directory.canonicalPath }.getOrElse { directory.absolutePath }
        val processLock = directoryLocks.getOrPut(directoryKey) { Any() }
        return synchronized(processLock) {
            reserveLocked(directory, localDate, authoritativeStoragePaths, minimumIndex)
        }
    }

    fun activeLeases(directory: File): ActiveLeases {
        val protectedPaths = LinkedHashSet<String>()
        val localLogPaths = LinkedHashSet<String>()
        directory.listFiles { file -> file.isFile && isLeaseName(file.name) }
            .orEmpty()
            .forEach { leaseFile ->
                val logName = logNameForLease(leaseFile.name) ?: return@forEach
                val localPath = File(directory, logName).absolutePath
                when (leaseState(leaseFile)) {
                    LeaseState.ACTIVE,
                    LeaseState.UNKNOWN -> {
                        localLogPaths += localPath
                        protectedPaths += localPath
                        protectedPaths += logName
                        readProtectedPath(leaseFile)?.let(protectedPaths::add)
                    }
                    LeaseState.STALE -> leaseFile.delete()
                }
            }
        return ActiveLeases(protectedPaths, localLogPaths)
    }

    private fun reserveLocked(
        directory: File,
        localDate: String,
        authoritativeStoragePaths: Collection<String>?,
        minimumIndex: Long,
    ): Allocation {
        val sequenceFile = File(directory, ".${SessionLogName.PREFIX}$localDate.seq")
        RandomAccessFile(sequenceFile, "rw").use { randomAccess ->
            val channel = randomAccess.channel
            val fileLock = channel.lock()
            try {
                val persistedNext = readPersistedNext(channel)
                val candidate = if (authoritativeStoragePaths == null) {
                    maxOf(minimumIndex, persistedNext ?: scanNextIndex(directory, localDate))
                } else {
                    val occupied = scanAuthoritativeIndices(directory, localDate, authoritativeStoragePaths)
                    if (!occupied.present) {
                        resetSequence(channel)
                        minimumIndex
                    } else {
                        maxOf(minimumIndex, persistedNext ?: 0L, occupied.nextIndex)
                    }
                }
                if (candidate == Long.MAX_VALUE) {
                    throw IOException("Jank Hunter session index exhausted for $localDate")
                }
                appendNext(channel, candidate + 1L)
                val fileName = SessionLogName.create(localDate, candidate)
                val lease = createLease(directory, fileName)
                return Allocation(fileName, localDate, candidate, lease)
            } finally {
                fileLock.release()
            }
        }
    }

    private fun scanAuthoritativeIndices(
        directory: File,
        localDate: String,
        storagePaths: Collection<String>,
    ): IndexScan {
        val localNames = directory.listFiles { file -> file.isFile }
            .orEmpty()
            .asSequence()
            .map(File::getName)
        val storageNames = storagePaths.asSequence().map { path -> File(path).name }
        val activeLeaseNames = activeLeases(directory).localLogPaths.asSequence().map { path -> File(path).name }
        val highest = (localNames + storageNames + activeLeaseNames)
            .mapNotNull(SessionLogName::parse)
            .filter { parsed -> parsed.localDate == localDate }
            .maxOfOrNull(SessionLogName.Parsed::index)
            ?: return IndexScan(present = false, nextIndex = 0L)
        return IndexScan(
            present = true,
            nextIndex = if (highest == Long.MAX_VALUE) Long.MAX_VALUE else highest + 1L,
        )
    }

    private fun readPersistedNext(channel: FileChannel): Long? {
        val size = channel.size()
        if (size == 0L) return null
        val completeSize = size - size % SEQUENCE_RECORD_BYTES

        val record = ByteBuffer.allocate(SEQUENCE_RECORD_BYTES).order(ByteOrder.LITTLE_ENDIAN)
        var position = 0L
        var previous = -1L
        while (position < completeSize) {
            record.clear()
            readFully(channel, record, position)
            record.flip()
            val next = record.long
            val inverted = record.long
            if (next <= 0L || inverted != next.inv() || next <= previous) {
                throw IOException("Corrupt Jank Hunter sequence: invalid monotonic record")
            }
            previous = next
            position += SEQUENCE_RECORD_BYTES
        }
        if (completeSize != size) {
            // reserve() persists the sequence before publishing a name, so an incomplete tail
            // can only belong to a reservation that was never handed to a writer.
            channel.truncate(completeSize)
            channel.force(true)
        }
        return previous.takeIf { it >= 0L }
    }

    private fun scanNextIndex(directory: File, localDate: String): Long {
        val highest = directory.listFiles { file -> file.isFile }
            .orEmpty()
            .mapNotNull { file -> SessionLogName.parse(file.name) }
            .filter { parsed -> parsed.localDate == localDate }
            .maxOfOrNull(SessionLogName.Parsed::index)
            ?: return 0L
        if (highest == Long.MAX_VALUE) return Long.MAX_VALUE
        return highest + 1L
    }

    private fun appendNext(channel: FileChannel, next: Long) {
        val record = ByteBuffer.allocate(SEQUENCE_RECORD_BYTES).order(ByteOrder.LITTLE_ENDIAN)
            .putLong(next)
            .putLong(next.inv())
        record.flip()
        channel.position(channel.size())
        while (record.hasRemaining()) channel.write(record)
        channel.force(true)
    }

    private fun resetSequence(channel: FileChannel) {
        channel.truncate(0L)
        channel.position(0L)
        channel.force(true)
    }

    private fun createLease(directory: File, fileName: String): SessionLease {
        val finalFile = File(directory, ".${fileName.removeSuffix(SessionLogName.SUFFIX)}.lease")
        if (finalFile.exists()) throw IOException("Jank Hunter lease already exists: ${finalFile.name}")
        val temporary = File(
            directory,
            "${finalFile.name}.tmp-${temporaryLeaseId.incrementAndGet()}",
        )
        if (!temporary.createNewFile()) throw IOException("Cannot create Jank Hunter lease: ${temporary.name}")

        val randomAccess = RandomAccessFile(temporary, "rw")
        val channel = randomAccess.channel
        var lock: FileLock? = null
        try {
            lock = channel.lock()
            writeProtectedPath(channel, fileName)
            if (!temporary.renameTo(finalFile)) {
                throw IOException("Cannot publish Jank Hunter lease: ${finalFile.name}")
            }
            return SessionLease(finalFile, randomAccess, channel, lock)
        } catch (error: Throwable) {
            runCatching { lock?.release() }
            runCatching { randomAccess.close() }
            temporary.delete()
            throw error
        }
    }

    private fun leaseState(file: File): LeaseState {
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

    private fun readProtectedPath(file: File): String? {
        return runCatching {
            val bytes = file.readBytes()
            if (bytes.isEmpty() || bytes.size > MAX_LEASE_PATH_BYTES) return@runCatching null
            String(bytes, StandardCharsets.UTF_8).takeIf { it.isNotBlank() }
        }.getOrNull()
    }

    private fun writeProtectedPath(channel: FileChannel, path: String) {
        val bytes = path.toByteArray(StandardCharsets.UTF_8)
        if (bytes.size > MAX_LEASE_PATH_BYTES) throw IOException("Jank Hunter lease path is too long")
        val buffer = ByteBuffer.wrap(bytes)
        channel.truncate(0L)
        channel.position(0L)
        while (buffer.hasRemaining()) channel.write(buffer)
        channel.force(true)
    }

    private fun readFully(channel: FileChannel, target: ByteBuffer, position: Long) {
        var offset = position
        while (target.hasRemaining()) {
            val read = channel.read(target, offset)
            if (read < 0) throw IOException("Unexpected EOF in Jank Hunter sequence")
            if (read == 0) throw IOException("Cannot make progress reading Jank Hunter sequence")
            offset += read
        }
    }

    private fun ensureDirectory(directory: File) {
        if (directory.isDirectory) return
        if (!directory.exists() && directory.mkdirs()) return
        throw IOException("Cannot create Jank Hunter metadata directory: $directory")
    }

    private fun isLeaseName(name: String): Boolean {
        return name.startsWith(".${SessionLogName.PREFIX}") && name.endsWith(".lease")
    }

    private fun logNameForLease(name: String): String? {
        if (!isLeaseName(name)) return null
        val stem = name.removePrefix(".").removeSuffix(".lease")
        return "$stem${SessionLogName.SUFFIX}".takeIf { SessionLogName.parse(it) != null }
    }

    class Allocation internal constructor(
        val fileName: String,
        val localDate: String,
        val index: Long,
        private val lease: SessionLease,
    ) : Closeable {
        fun updateProtectedPath(path: String) = lease.updateProtectedPath(path)

        override fun close() = lease.close()
    }

    data class ActiveLeases(
        val protectedPaths: Set<String>,
        val localLogPaths: Set<String>,
    )

    private data class IndexScan(
        val present: Boolean,
        val nextIndex: Long,
    )

    private enum class LeaseState {
        ACTIVE,
        STALE,
        UNKNOWN,
    }

    internal class SessionLease(
        private val file: File,
        private val randomAccess: RandomAccessFile,
        private val channel: FileChannel,
        private val lock: FileLock,
    ) : Closeable {
        @Synchronized
        fun updateProtectedPath(path: String) = writeProtectedPath(channel, path)

        @Synchronized
        override fun close() {
            runCatching { lock.release() }
            runCatching { randomAccess.close() }
            file.delete()
        }
    }
}

internal object SessionLogRetention {
    fun enforce(directory: File, currentFile: File, historyLimitBytes: Long) {
        if (historyLimitBytes <= 0L) return
        val active = SessionLogAllocator.activeLeases(directory).localLogPaths
        val currentPath = currentFile.absolutePath
        val logs = directory.listFiles { file -> file.isFile && SessionLogName.parse(file.name) != null }
            .orEmpty()
            .toList()
        var totalBytes = logs.sumOf(File::length)
        if (totalBytes <= historyLimitBytes) return

        val candidates = logs.asSequence()
            .filterNot { file -> file.absolutePath == currentPath || file.absolutePath in active }
            .sortedWith(
                compareBy<File> { file -> SessionLogName.parse(file.name)?.localDate.orEmpty() }
                    .thenBy { file -> SessionLogName.parse(file.name)?.index ?: Long.MAX_VALUE }
                    .thenBy(File::lastModified),
            )
        for (file in candidates) {
            if (totalBytes <= historyLimitBytes) return
            val bytes = file.length()
            if (file.delete()) totalBytes -= bytes
        }
    }
}
