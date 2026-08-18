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
    private const val LOCAL_DATE_LENGTH = 10
    private const val RUN_ID_BYTES = 16
    private const val RUN_ID_HEX_LENGTH = RUN_ID_BYTES * 2

    fun create(localDate: String, runId: ByteArray, index: Long): String {
        require(isLocalDate(localDate)) { "session log date must use yyyy-MM-dd" }
        require(runId.size == RUN_ID_BYTES) { "session log run ID must have $RUN_ID_BYTES bytes" }
        require(runId.any { value -> value != 0.toByte() }) { "session log run ID must not be zero" }
        require(index >= 0L) { "session log index must be non-negative" }
        return "$PREFIX$localDate.${runIdHex(runId)}.$index$SUFFIX"
    }

    fun runIdHex(runId: ByteArray): String {
        require(runId.size == RUN_ID_BYTES) { "session log run ID must have $RUN_ID_BYTES bytes" }
        return runId.toHex()
    }

    fun parse(fileName: String): Parsed? {
        if (!fileName.startsWith(PREFIX) || !fileName.endsWith(SUFFIX)) return null
        val body = fileName.removePrefix(PREFIX).removeSuffix(SUFFIX)
        parseLegacy(body)?.let { return it }
        val runSeparator = LOCAL_DATE_LENGTH
        val indexSeparator = runSeparator + 1 + RUN_ID_HEX_LENGTH
        if (body.length <= indexSeparator + 1 || body[runSeparator] != '.' || body[indexSeparator] != '.') {
            return null
        }
        val localDate = body.substring(0, runSeparator)
        if (!isLocalDate(localDate)) return null
        val runId = body.substring(runSeparator + 1, indexSeparator)
        if (!runId.all(::isLowerHex) || runId.all { value -> value == '0' }) return null
        val indexText = body.substring(indexSeparator + 1)
        if (indexText.length > 1 && indexText[0] == '0') return null
        val index = indexText.toLongOrNull()?.takeIf { it >= 0L } ?: return null
        return Parsed(localDate, runId, index)
    }

    data class Parsed(
        val localDate: String,
        val runId: String?,
        val index: Long,
    ) {
        val retentionUnitId: String
            get() = runId ?: "legacy:$localDate:$index"
    }

    private fun parseLegacy(body: String): Parsed? {
        if (body.length <= LOCAL_DATE_LENGTH + 1 || body[LOCAL_DATE_LENGTH] != '.') return null
        val localDate = body.substring(0, LOCAL_DATE_LENGTH)
        if (!isLocalDate(localDate)) return null
        val indexText = body.substring(LOCAL_DATE_LENGTH + 1)
        if (indexText.length > 1 && indexText[0] == '0') return null
        val index = indexText.toLongOrNull()?.takeIf { it >= 0L } ?: return null
        return Parsed(localDate, runId = null, index)
    }

    private fun isLocalDate(value: String): Boolean {
        if (value.length != LOCAL_DATE_LENGTH || value[4] != '-' || value[7] != '-') return false
        if (!value.indices.all { index -> index == 4 || index == 7 || value[index] in '0'..'9' }) return false
        val year = value.decimal(0, 4)
        val month = value.decimal(5, 7)
        val day = value.decimal(8, 10)
        if (month !in 1..12) return false
        val days = when (month) {
            2 -> if (year % 4 == 0 && (year % 100 != 0 || year % 400 == 0)) 29 else 28
            4, 6, 9, 11 -> 30
            else -> 31
        }
        return day in 1..days
    }

    private fun String.decimal(start: Int, end: Int): Int {
        var value = 0
        for (index in start until end) value = value * 10 + (this[index] - '0')
        return value
    }

    private fun isLowerHex(value: Char): Boolean = value in '0'..'9' || value in 'a'..'f'

    private fun ByteArray.toHex(): String {
        val chars = CharArray(size * 2)
        for (index in indices) {
            val value = this[index].toInt() and 0xff
            chars[index * 2] = HEX[value ushr 4]
            chars[index * 2 + 1] = HEX[value and 0x0f]
        }
        return String(chars)
    }

    private val HEX = "0123456789abcdef".toCharArray()
}

internal object SessionLogAllocator {
    private const val SEQUENCE_RECORD_BYTES = Long.SIZE_BYTES * 2
    private const val MAX_LEASE_PATH_BYTES = 64 * 1024
    private val directoryLocks = ConcurrentHashMap<String, Any>()
    private val temporaryLeaseId = AtomicLong()

    fun reserve(
        directory: File,
        localDate: String,
        runId: ByteArray,
        authoritativeStoragePaths: Collection<String>? = null,
        minimumIndex: Long = 0L,
    ): Allocation {
        SessionLogName.create(localDate, runId, 0L)
        require(minimumIndex >= 0L) { "minimum session log index must be non-negative" }
        ensureDirectory(directory)
        val directoryKey = runCatching { directory.canonicalPath }.getOrElse { directory.absolutePath }
        val processLock = directoryLocks.getOrPut(directoryKey) { Any() }
        return synchronized(processLock) {
            reserveLocked(directory, localDate, runId, authoritativeStoragePaths, minimumIndex)
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
        runId: ByteArray,
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
                val fileName = SessionLogName.create(localDate, runId, candidate)
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
    fun enforce(
        directory: File,
        currentRunId: String,
        protectedPaths: Set<String>,
        historyLimitBytes: Long,
    ): Result = enforce(
        paths = directory.listFiles { file -> file.isFile }
            .orEmpty()
            .map(File::getAbsolutePath),
        currentRunId = currentRunId,
        protectedPaths = protectedPaths,
        historyLimitBytes = historyLimitBytes,
        delete = { artifact -> artifact.file.delete() },
    )

    fun enforce(
        storage: io.jankhunter.runtime.JankHunterBinaryStorage,
        currentRunId: String,
        protectedPaths: Set<String>,
        historyLimitBytes: Long,
    ): Result = enforce(
        paths = storage.listFiles(),
        currentRunId = currentRunId,
        protectedPaths = protectedPaths,
        historyLimitBytes = historyLimitBytes,
        delete = { artifact ->
            storage.delete(artifact.file.name)
            !artifact.file.exists()
        },
    )

    private fun enforce(
        paths: List<String>,
        currentRunId: String,
        protectedPaths: Set<String>,
        historyLimitBytes: Long,
        delete: (Artifact) -> Boolean,
    ): Result {
        if (historyLimitBytes <= 0L || historyLimitBytes == Long.MAX_VALUE) return Result.EMPTY
        val units = LinkedHashMap<String, RetentionUnit>()
        var totalBytes = 0L
        var currentRunBytes = 0L
        paths.forEach { path ->
            val file = File(path)
            val parsed = SessionLogName.parse(file.name) ?: return@forEach
            val bytes = file.length().coerceAtLeast(0L)
            totalBytes = saturatedAdd(totalBytes, bytes)
            if (parsed.runId == currentRunId) currentRunBytes = saturatedAdd(currentRunBytes, bytes)
            val retentionUnitId = parsed.retentionUnitId
            val unit = units.getOrPut(retentionUnitId) { RetentionUnit(retentionUnitId) }
            unit.artifacts += Artifact(file, bytes)
            unit.order = maxOf(unit.order, file.lastModified())
            if (
                parsed.runId == currentRunId ||
                file.absolutePath in protectedPaths ||
                file.name in protectedPaths ||
                path in protectedPaths
            ) {
                unit.protected = true
            }
        }
        val totalBefore = totalBytes
        if (totalBytes <= historyLimitBytes) {
            return Result(totalBefore, totalBytes, 0L, 0L, 0L, currentRunBytes, fits = true)
        }

        val candidates = units.values.asSequence()
            .filterNot(RetentionUnit::protected)
            .sortedWith(compareBy<RetentionUnit>(RetentionUnit::order).thenBy(RetentionUnit::runId))
        var deletedRuns = 0L
        var deletedSegments = 0L
        for (unit in candidates) {
            if (totalBytes <= historyLimitBytes) break
            var complete = true
            unit.artifacts.forEach { artifact ->
                if (delete(artifact)) {
                    totalBytes = (totalBytes - artifact.bytes).coerceAtLeast(0L)
                    deletedSegments++
                } else {
                    complete = false
                }
            }
            if (complete) deletedRuns++
        }
        return Result(
            totalBefore = totalBefore,
            totalAfter = totalBytes,
            deletedBytes = (totalBefore - totalBytes).coerceAtLeast(0L),
            deletedRuns = deletedRuns,
            deletedSegments = deletedSegments,
            currentRunBytes = currentRunBytes,
            fits = totalBytes <= historyLimitBytes,
        )
    }

    data class Result(
        val totalBefore: Long,
        val totalAfter: Long,
        val deletedBytes: Long,
        val deletedRuns: Long,
        val deletedSegments: Long,
        val currentRunBytes: Long,
        val fits: Boolean,
    ) {
        companion object {
            val EMPTY = Result(0L, 0L, 0L, 0L, 0L, 0L, fits = true)
        }
    }

    private data class Artifact(
        val file: File,
        val bytes: Long,
    )

    private class RetentionUnit(val runId: String) {
        val artifacts = ArrayList<Artifact>()
        var order = Long.MIN_VALUE
        var protected = false
    }
}
