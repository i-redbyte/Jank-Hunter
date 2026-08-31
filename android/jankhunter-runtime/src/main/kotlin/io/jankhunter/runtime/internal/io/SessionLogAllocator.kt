package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.channels.FileChannel
import java.nio.channels.FileLock
import java.nio.channels.OverlappingFileLockException
import java.nio.charset.StandardCharsets
import java.util.concurrent.atomic.AtomicLong

internal object SessionLogName {
    const val BASE_PREFIX = "jh-session-log."
    const val PREFIX = BASE_PREFIX
    const val SUFFIX = ".jhlog"
    private const val SEQUENCE_PREFIX = ".jh-session-index."
    private const val SEQUENCE_SUFFIX = ".seq"
    private const val LOCAL_DATE_LENGTH = 10
    private const val RUN_ID_BYTES = 16
    private const val RUN_ID_HEX_LENGTH = RUN_ID_BYTES * 2

    fun create(
        localDate: String,
        runId: ByteArray,
        dailySessionIndex: Long,
        segmentIndex: Long,
    ): String {
        require(isLocalDate(localDate)) { "session log date must use yyyy-MM-dd" }
        require(runId.size == RUN_ID_BYTES) { "session log run ID must have $RUN_ID_BYTES bytes" }
        require(runId.any { value -> value != 0.toByte() }) { "session log run ID must not be zero" }
        require(dailySessionIndex >= 0L) { "daily session index must be non-negative" }
        require(segmentIndex >= 0L) { "session log segment index must be non-negative" }
        val segmentSuffix = if (segmentIndex == 0L) "" else "-$segmentIndex"
        return "$PREFIX$localDate.${runIdHex(runId)}.$dailySessionIndex$segmentSuffix$SUFFIX"
    }

    fun sequenceFileName(localDate: String): String {
        require(isLocalDate(localDate)) { "session log date must use yyyy-MM-dd" }
        return "$SEQUENCE_PREFIX$localDate$SEQUENCE_SUFFIX"
    }

    fun runIdHex(runId: ByteArray): String {
        require(runId.size == RUN_ID_BYTES) { "session log run ID must have $RUN_ID_BYTES bytes" }
        return runId.toHex()
    }

    fun parse(fileName: String): Parsed? {
        if (!fileName.startsWith(PREFIX) || !fileName.endsWith(SUFFIX)) return null
        val body = fileName.removePrefix(PREFIX).removeSuffix(SUFFIX)
        val runSeparator = LOCAL_DATE_LENGTH
        val indexSeparator = runSeparator + 1 + RUN_ID_HEX_LENGTH
        if (body.length <= indexSeparator + 1 || body[runSeparator] != '.' || body[indexSeparator] != '.') {
            return null
        }
        val localDate = body.substring(0, runSeparator)
        if (!isLocalDate(localDate)) return null
        val runId = body.substring(runSeparator + 1, indexSeparator)
        if (!runId.all(::isLowerHex) || runId.all { value -> value == '0' }) return null
        val dailyIndexStart = indexSeparator + 1
        val segmentSeparator = body.indexOf('-', dailyIndexStart)
        val dailyIndexEnd = if (segmentSeparator < 0) body.length else segmentSeparator
        val dailySessionIndex = body.canonicalIndex(dailyIndexStart, dailyIndexEnd) ?: return null
        val segmentIndex = if (segmentSeparator < 0) {
            0L
        } else {
            body.canonicalIndex(segmentSeparator + 1, body.length)?.takeIf { it > 0L } ?: return null
        }
        return Parsed(localDate, runId, dailySessionIndex, segmentIndex)
    }

    fun isJhlogArtifact(fileName: String): Boolean {
        return fileName.startsWith(BASE_PREFIX) && fileName.endsWith(SUFFIX)
    }

    data class Parsed(
        val localDate: String,
        val runId: String,
        val dailySessionIndex: Long,
        val segmentIndex: Long,
    ) {
        val retentionUnitId: String
            get() = runId
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

    private fun String.canonicalIndex(start: Int, end: Int): Long? {
        if (start >= end || end - start > MAX_LONG_DECIMAL_DIGITS) return null
        if (this[start] == '0' && end - start > 1) return null
        var result = 0L
        for (index in start until end) {
            val digit = this[index] - '0'
            if (digit !in 0..9 || result > (Long.MAX_VALUE - digit) / 10L) return null
            result = result * 10L + digit
        }
        return result
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
    private const val MAX_LONG_DECIMAL_DIGITS = 19
}

internal object SessionLogAllocator {
    private const val SEGMENT_LOCK_FILE_NAME = ".jh-session-segment.lock"
    private const val MAX_LEASE_PATH_BYTES = 64 * 1024
    private val temporaryLeaseId = AtomicLong()

    fun reserve(
        directory: File,
        localDate: String,
        runId: ByteArray,
        dailySessionIndex: Long,
        authoritativeStoragePaths: Collection<String>? = null,
        minimumSegmentIndex: Long = 0L,
    ): Allocation {
        SessionLogName.create(localDate, runId, dailySessionIndex, minimumSegmentIndex)
        require(minimumSegmentIndex >= 0L) { "minimum session log segment index must be non-negative" }
        ensureDirectory(directory)
        return CrossProcessFileLocks.withDirectoryLock(directory, SEGMENT_LOCK_FILE_NAME) {
            reserveLocked(
                directory,
                localDate,
                runId,
                dailySessionIndex,
                authoritativeStoragePaths,
                minimumSegmentIndex,
            )
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
        dailySessionIndex: Long,
        authoritativeStoragePaths: Collection<String>?,
        minimumSegmentIndex: Long,
    ): Allocation {
        val highest = scanHighestSegmentIndex(
            directory,
            localDate,
            SessionLogName.runIdHex(runId),
            dailySessionIndex,
            authoritativeStoragePaths,
        )
        val next = when (highest) {
            null -> 0L
            Long.MAX_VALUE -> throw IOException("Jank Hunter segment index exhausted for run ${SessionLogName.runIdHex(runId)}")
            else -> highest + 1L
        }
        val segmentIndex = maxOf(minimumSegmentIndex, next)
        val fileName = SessionLogName.create(localDate, runId, dailySessionIndex, segmentIndex)
        val lease = createLease(directory, fileName)
        return Allocation(fileName, localDate, dailySessionIndex, segmentIndex, lease)
    }

    private fun scanHighestSegmentIndex(
        directory: File,
        localDate: String,
        runId: String,
        dailySessionIndex: Long,
        storagePaths: Collection<String>?,
    ): Long? {
        val localNames = directory.listFiles { file -> file.isFile }
            .orEmpty()
            .asSequence()
            .map(File::getName)
        val storageNames = storagePaths.orEmpty().asSequence().map { path -> File(path).name }
        val activeLeaseNames = activeLeases(directory).localLogPaths.asSequence().map { path -> File(path).name }
        return (localNames + storageNames + activeLeaseNames)
            .mapNotNull(SessionLogName::parse)
            .filter { parsed ->
                parsed.localDate == localDate &&
                    parsed.runId == runId &&
                    parsed.dailySessionIndex == dailySessionIndex
            }
            .maxOfOrNull(SessionLogName.Parsed::segmentIndex)
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

    private fun ensureDirectory(directory: File) {
        if (directory.isDirectory) return
        if (!directory.exists() && directory.mkdirs()) return
        throw IOException("Cannot create Jank Hunter metadata directory: $directory")
    }

    private fun isLeaseName(name: String): Boolean {
        return name.startsWith(".${SessionLogName.BASE_PREFIX}") && name.endsWith(".lease")
    }

    private fun logNameForLease(name: String): String? {
        if (!isLeaseName(name)) return null
        val stem = name.removePrefix(".").removeSuffix(".lease")
        return "$stem${SessionLogName.SUFFIX}".takeIf { it.startsWith(SessionLogName.BASE_PREFIX) }
    }

    class Allocation internal constructor(
        val fileName: String,
        val localDate: String,
        val dailySessionIndex: Long,
        val segmentIndex: Long,
        private val lease: SessionLease,
    ) : Closeable {
        fun updateProtectedPath(path: String) = lease.updateProtectedPath(path)

        override fun close() = lease.close()
    }

    data class ActiveLeases(
        val protectedPaths: Set<String>,
        val localLogPaths: Set<String>,
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

internal object ObsoleteSessionLogCleaner {
    fun clean(
        directory: File,
        storage: io.jankhunter.runtime.JankHunterBinaryStorage?,
    ): Result {
        val protectedPaths = SessionLogAllocator.activeLeases(directory).protectedPaths
        val processedPaths = HashSet<String>()
        var deleted = 0L
        var failed = 0L
        var protected = 0L
        val fileMagic = ByteArray(Jhlog.FILE_MAGIC.size)

        fun isProtected(path: String, fileName: String): Boolean {
            return path in protectedPaths || File(path).absolutePath in protectedPaths || fileName in protectedPaths
        }

        directory.listFiles { file -> file.isFile }
            .orEmpty()
            .forEach { file ->
                val path = file.absolutePath
                processedPaths += path
                if (!isObsoleteJhlog(file, fileMagic) && !isObsoleteSequence(file.name)) return@forEach
                if (isProtected(path, file.name)) {
                    protected++
                } else if (file.delete()) {
                    deleted++
                } else {
                    failed++
                }
            }

        storage?.listFiles()?.forEach { path ->
            val file = File(path)
            val absolutePath = file.absolutePath
            if (!processedPaths.add(absolutePath) || !isObsoleteJhlog(file, fileMagic)) return@forEach
            if (isProtected(path, file.name)) {
                protected++
                return@forEach
            }
            val removed = runCatching {
                storage.delete(file.name)
                !file.exists()
            }.getOrDefault(false)
            if (removed) deleted++ else failed++
        }
        return Result(deleted, failed, protected)
    }

    private fun isObsoleteJhlog(file: File, fileMagic: ByteArray): Boolean {
        if (!SessionLogName.isJhlogArtifact(file.name)) return false
        return runCatching {
            if (!file.isFile || file.length() < fileMagic.size) return@runCatching false
            RandomAccessFile(file, "r").use { randomAccess -> randomAccess.readFully(fileMagic) }
            Jhlog.isKnownObsoleteFileMagic(fileMagic)
        }.getOrDefault(false)
    }

    private fun isObsoleteSequence(fileName: String): Boolean {
        if (!fileName.endsWith(".seq")) return false
        val legacyPrefix = ".${SessionLogName.BASE_PREFIX}"
        return fileName.startsWith("${legacyPrefix}v2.") ||
            fileName.startsWith(legacyPrefix) && !fileName.startsWith("${legacyPrefix}v")
    }

    data class Result(
        val deleted: Long,
        val failed: Long,
        val protected: Long,
    )
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
