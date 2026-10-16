package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterStoragePolicy
import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.BufferedInputStream
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.zip.CRC32
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream

/** Converts inactive session directories into verified, atomically published archive units. */
internal object SessionArchiveCoordinator {
    fun isPublishedArchive(archive: File): Boolean {
        if (!archive.isFile || !archive.name.endsWith(ARCHIVE_SUFFIX)) return false
        val sessionName = archive.name.removeSuffix(ARCHIVE_SUFFIX)
        val identity = SessionArtifactPath.parseSessionDirectoryName(sessionName) ?: return false
        return verifyPublishedArchive(archive, identity)
    }

    fun maintain(
        root: File,
        currentRunId: String,
        faultInjector: FaultInjector = FaultInjector.NONE,
        policy: JankHunterStoragePolicy? = null,
    ): Result {
        require(SessionArtifactPath.isCanonicalId(currentRunId)) { "current run ID must be canonical" }
        if (!root.isDirectory && !root.mkdirs() && !root.isDirectory) {
            throw IOException("Cannot create Jank Hunter artifact root: $root")
        }
        return CrossProcessFileLocks.withDirectoryLock(root, LOCK_FILE_NAME) {
            if (policy == null) {
                SessionArtifactReadLeases.mutate(root, Result(0L, 0L, 0L, 0L)) {
                    maintainLocked(root, currentRunId, faultInjector, null)
                }
            } else {
                maintainLocked(root, currentRunId, faultInjector, policy)
            }
        }
    }

    private fun maintainLocked(root: File, currentRunId: String, faultInjector: FaultInjector, policy: JankHunterStoragePolicy?): Result {
        val activeRunIds = ProcessRunCohort.activeRunIds(root)
        var archived = 0L
        var recovered = 0L
        var active = 0L
        var incomplete = 0L
        val sessions = root.listFiles { file ->
            file.isDirectory && SessionArtifactPath.parseSessionDirectoryName(file.name) != null
        }.orEmpty().sortedBy(File::getName)
        for (session in sessions) {
            val identity = checkNotNull(SessionArtifactPath.parseSessionDirectoryName(session.name))
            if (identity.runId == currentRunId || identity.runId in activeRunIds) {
                active++
                continue
            }
            val outcome = if (policy == null) archiveSession(root, session, identity, faultInjector)
                else archiveSessionWithPolicy(root, session, identity, faultInjector, policy)
            when (outcome) {
                ArchiveOutcome.ARCHIVED -> archived++
                ArchiveOutcome.RECOVERED -> recovered++
                ArchiveOutcome.INCOMPLETE -> incomplete++
            }
        }
        incomplete += pruneHistoricalArchiveHeapDumps(root, currentRunId, policy, ::verifyPublishedArchive)
        return Result(archived, recovered, active, incomplete)
    }

    private fun archiveSessionWithPolicy(
        root: File,
        session: File,
        identity: SessionArtifactPath.ParsedSession,
        faultInjector: FaultInjector,
        policy: JankHunterStoragePolicy,
    ): ArchiveOutcome {
        if (session.canonicalFile.parentFile != root.canonicalFile) return ArchiveOutcome.INCOMPLETE
        val target = File(root, "${session.name}$ARCHIVE_SUFFIX")
        val temporary = File(root, ".${session.name}$TEMP_SUFFIX")
        var recovered = false
        var alreadyPublished = false
        SessionArtifactReadLeases.acquire(root).use {
            if (target.exists()) {
                if (!verifyPublishedArchive(target, identity)) return ArchiveOutcome.INCOMPLETE
                alreadyPublished = true
                recovered = true
            } else {
                val sources = runCatching { validatedSources(session, identity) }.getOrElse {
                    if (it is VirtualMachineError || it is ThreadDeath) throw it
                    return ArchiveOutcome.INCOMPLETE
                }
                recovered = temporary.exists() && verifyArchive(temporary, sources)
                if (!recovered) {
                    writeArchive(temporary, sources)
                    if (!verifyArchive(temporary, sources)) throw IOException("Cannot verify session archive")
                }
                faultInjector.onPhase(FaultPhase.BEFORE_PUBLISH)
            }
        }
        val published = SessionStorageBudget.publishArchive(root, policy,
            if (alreadyPublished) null else temporary, target, session) {
            if (!session.isDirectory || alreadyPublished && !target.isFile) return@publishArchive false
            if (!alreadyPublished) publish(temporary, target)
            faultInjector.onPhase(FaultPhase.AFTER_PUBLISH)
            deleteSessionTree(session, faultInjector)
            temporary.delete()
            true
        }
        if (!published) temporary.delete()
        return when {
            !published -> ArchiveOutcome.INCOMPLETE
            recovered -> ArchiveOutcome.RECOVERED
            else -> ArchiveOutcome.ARCHIVED
        }
    }

    /** Pressure-only removal of an inactive complete session, called under the mutation gate. */
    fun reclaimCompletedSession(session: File, identity: SessionArtifactPath.ParsedSession): Boolean = try {
        validatedSources(session, identity)
        deleteSessionTree(session, FaultInjector.NONE)
        true
    } catch (_: IOException) {
        false
    }

    private fun archiveSession(
        root: File,
        session: File,
        identity: SessionArtifactPath.ParsedSession,
        faultInjector: FaultInjector,
    ): ArchiveOutcome {
        if (session.canonicalFile.parentFile != root.canonicalFile) return ArchiveOutcome.INCOMPLETE
        val target = File(root, "${session.name}$ARCHIVE_SUFFIX")
        val temporary = File(root, ".${session.name}$TEMP_SUFFIX")
        if (target.exists()) {
            if (!verifyPublishedArchive(target, identity)) return ArchiveOutcome.INCOMPLETE
            faultInjector.onPhase(FaultPhase.AFTER_PUBLISH)
            deleteSessionTree(session, faultInjector)
            temporary.delete()
            return ArchiveOutcome.RECOVERED
        }
        val sources = runCatching { validatedSources(session, identity) }.getOrElse {
            if (it is VirtualMachineError || it is ThreadDeath) throw it
            return ArchiveOutcome.INCOMPLETE
        }
        if (temporary.exists()) {
            if (!verifyArchive(temporary, sources)) {
                if (!temporary.delete()) return ArchiveOutcome.INCOMPLETE
            } else {
                publish(temporary, target)
                faultInjector.onPhase(FaultPhase.AFTER_PUBLISH)
                deleteSessionTree(session, faultInjector)
                return ArchiveOutcome.RECOVERED
            }
        }
        writeArchive(temporary, sources)
        if (!verifyArchive(temporary, sources)) throw IOException("Cannot verify Jank Hunter session archive ${session.name}")
        faultInjector.onPhase(FaultPhase.BEFORE_PUBLISH)
        publish(temporary, target)
        faultInjector.onPhase(FaultPhase.AFTER_PUBLISH)
        deleteSessionTree(session, faultInjector)
        return ArchiveOutcome.ARCHIVED
    }

    private fun validatedSources(
        session: File,
        identity: SessionArtifactPath.ParsedSession,
    ): List<ArchiveSource> {
        val sessionCanonical = session.canonicalFile
        val sources = ArrayList<ArchiveSource>()
        var jhlogCount = 0
        val processDirectories = session.listFiles().orEmpty()
        if (processDirectories.isEmpty() || processDirectories.size > MAX_PROCESS_DIRECTORIES) {
            throw IOException("Invalid Jank Hunter process directory count")
        }
        for (process in processDirectories) {
            if (!process.isDirectory || !SessionArtifactPath.isCanonicalId(process.name)) {
                throw IOException("Invalid Jank Hunter process artifact directory ${process.name}")
            }
            if (process.canonicalFile.parentFile != sessionCanonical) throw IOException("Escaped process artifact directory")
            val artifacts = process.listFiles().orEmpty()
            if (artifacts.isEmpty() || artifacts.size > MAX_ARTIFACTS_PER_PROCESS) {
                throw IOException("Invalid Jank Hunter process artifact count")
            }
            for (artifact in artifacts) {
                if (!artifact.isFile || artifact.canonicalFile.parentFile != process.canonicalFile) {
                    throw IOException("Invalid Jank Hunter session artifact ${artifact.name}")
                }
                if (isManagedPendingHeap(artifact.name)) continue
                val parsed = SessionLogName.parse(artifact.name)
                if (parsed != null) {
                    if (parsed.runId != identity.runId || parsed.dailySessionIndex != identity.dailySessionIndex) {
                        throw IOException("JHLOG identity does not match session directory")
                    }
                    val header = readJhlogIdentity(artifact)
                    if (
                        header.runId != identity.runId ||
                        header.processInstanceId != process.name ||
                        !header.processRecording && header.segmentIndex != parsed.segmentIndex
                    ) {
                        throw IOException("JHLOG header does not match artifact path")
                    }
                    jhlogCount++
                } else if (!RetainedHeapDumper.isManagedHeapDumpFileName(artifact.name)) {
                    throw IOException("Unknown Jank Hunter session artifact ${artifact.name}")
                }
                if (sources.size == MAX_ARCHIVE_ENTRIES) throw IOException("Jank Hunter archive entry limit exceeded")
                // All archived directories belong to earlier launches. Their dumps are obsolete.
                if (parsed != null) sources += archiveSource(artifact, "${process.name}/${artifact.name}")
            }
        }
        if (jhlogCount == 0) throw IOException("Jank Hunter session has no JHLOG")
        sources.sortBy(ArchiveSource::entryName)
        return sources
    }

    private fun archiveSource(file: File, entryName: String): ArchiveSource {
        val crc = CRC32()
        var size = 0L
        val buffer = ByteArray(COPY_BUFFER_BYTES)
        BufferedInputStream(FileInputStream(file), COPY_BUFFER_BYTES).use { input ->
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                if (count == 0) continue
                crc.update(buffer, 0, count)
                size = Math.addExact(size, count.toLong())
            }
        }
        if (size != file.length()) throw IOException("Jank Hunter artifact changed while scanning: $file")
        return ArchiveSource(file, entryName, size, crc.value)
    }

    private fun writeArchive(temporary: File, sources: List<ArchiveSource>) {
        if (temporary.exists() && !temporary.delete()) throw IOException("Cannot replace archive temporary file")
        var complete = false
        try {
            FileOutputStream(temporary).use { fileOutput ->
                ZipOutputStream(fileOutput.buffered(COPY_BUFFER_BYTES)).use { output ->
                    val buffer = ByteArray(COPY_BUFFER_BYTES)
                    for (source in sources) {
                        val entry = ZipEntry(source.entryName).apply {
                            method = ZipEntry.STORED
                            size = source.size
                            compressedSize = source.size
                            crc = source.crc
                            time = 0L
                        }
                        output.putNextEntry(entry)
                        BufferedInputStream(FileInputStream(source.file), COPY_BUFFER_BYTES).use { input ->
                            while (true) {
                                val count = input.read(buffer)
                                if (count < 0) break
                                if (count > 0) output.write(buffer, 0, count)
                            }
                        }
                        output.closeEntry()
                    }
                }
            }
            RandomAccessFile(temporary, "rw").use { file -> file.fd.sync() }
            complete = true
        } finally {
            if (!complete) temporary.delete()
        }
    }

    private fun verifyArchive(archive: File, sources: List<ArchiveSource>): Boolean = runCatching {
        if (!archive.isFile) return@runCatching false
        ZipFile(archive).use archiveUse@{ zip ->
            val entries = zip.entries()
            var index = 0
            val buffer = ByteArray(COPY_BUFFER_BYTES)
            while (entries.hasMoreElements()) {
                if (index >= sources.size) return@archiveUse false
                val entry = entries.nextElement()
                val source = sources[index]
                if (
                    entry.isDirectory ||
                    entry.name != source.entryName ||
                    entry.method != ZipEntry.STORED ||
                    entry.size != source.size ||
                    entry.crc != source.crc
                ) return@archiveUse false
                val crc = CRC32()
                var size = 0L
                zip.getInputStream(entry).use { input ->
                    while (true) {
                        val count = input.read(buffer)
                        if (count < 0) break
                        if (count > 0) {
                            crc.update(buffer, 0, count)
                            size = Math.addExact(size, count.toLong())
                        }
                    }
                }
                if (size != source.size || crc.value != source.crc) return@archiveUse false
                index++
            }
            index == sources.size
        }
    }.getOrDefault(false)

    internal fun verifyPublishedArchive(
        archive: File,
        identity: SessionArtifactPath.ParsedSession,
    ): Boolean = runCatching {
        if (!archive.isFile) return@runCatching false
        ZipFile(archive).use archiveUse@{ zip ->
            val entries = zip.entries()
            val names = HashSet<String>()
            val buffer = ByteArray(COPY_BUFFER_BYTES)
            var entryCount = 0
            var jhlogCount = 0
            while (entries.hasMoreElements()) {
                if (entryCount == MAX_ARCHIVE_ENTRIES) return@archiveUse false
                val entry = entries.nextElement()
                if (entry.isDirectory || entry.method != ZipEntry.STORED || !names.add(entry.name)) {
                    return@archiveUse false
                }
                val separator = entry.name.indexOf('/')
                if (separator <= 0 || separator != entry.name.lastIndexOf('/')) return@archiveUse false
                val processId = entry.name.substring(0, separator)
                val artifactName = entry.name.substring(separator + 1)
                if (!SessionArtifactPath.isCanonicalId(processId)) return@archiveUse false
                zip.getInputStream(entry).buffered(COPY_BUFFER_BYTES).use { input ->
                    val parsed = SessionLogName.parse(artifactName)
                    if (parsed != null) {
                        if (parsed.runId != identity.runId || parsed.dailySessionIndex != identity.dailySessionIndex) {
                            return@archiveUse false
                        }
                        val header = readJhlogIdentity(input)
                        if (
                            header.runId != identity.runId ||
                            header.processInstanceId != processId ||
                            !header.processRecording && header.segmentIndex != parsed.segmentIndex
                        ) return@archiveUse false
                        jhlogCount++
                    } else if (!RetainedHeapDumper.isManagedHeapDumpFileName(artifactName)) {
                        return@archiveUse false
                    }
                    while (true) {
                        val count = input.read(buffer)
                        if (count < 0) break
                    }
                }
                entryCount++
            }
            entryCount > 0 && jhlogCount > 0
        }
    }.getOrDefault(false)

    private fun publish(temporary: File, target: File) {
        if (target.exists()) throw IOException("Jank Hunter session archive already exists: $target")
        if (!temporary.renameTo(target)) throw IOException("Cannot publish Jank Hunter session archive: $target")
    }

    private fun deleteSessionTree(session: File, faultInjector: FaultInjector) {
        val processDirectories = session.listFiles().orEmpty()
        for (process in processDirectories) {
            if (!process.isDirectory || !SessionArtifactPath.isCanonicalId(process.name)) {
                throw IOException("Cannot delete unknown session path: $process")
            }
            for (artifact in process.listFiles().orEmpty()) {
                if (
                    !artifact.isFile ||
                    SessionLogName.parse(artifact.name) == null &&
                    !RetainedHeapDumper.isManagedHeapDumpFileName(artifact.name) &&
                    !isManagedPendingHeap(artifact.name)
                ) {
                    throw IOException("Cannot delete unknown session artifact: $artifact")
                }
                if (!artifact.delete()) throw IOException("Cannot delete archived Jank Hunter artifact: $artifact")
                faultInjector.onPhase(FaultPhase.AFTER_SOURCE_ARTIFACT_DELETE)
            }
            if (!process.delete()) throw IOException("Cannot delete archived Jank Hunter process directory: $process")
        }
        if (!session.delete()) throw IOException("Cannot delete archived Jank Hunter session directory: $session")
    }

    private fun isManagedPendingHeap(name: String): Boolean {
        val prefix = ".jh-heap-"
        val suffix = ".pending"
        if (!name.startsWith(prefix) || !name.endsWith(suffix)) return false
        val end = name.length - suffix.length
        if (end - prefix.length !in 1..20) return false
        for (index in prefix.length until end) if (name[index] !in '0'..'9') return false
        return true
    }

    private fun readJhlogIdentity(file: File): JhlogIdentity = FileInputStream(file).buffered(COPY_BUFFER_BYTES).use {
        readJhlogIdentity(it)
    }

    private fun readJhlogIdentity(input: InputStream): JhlogIdentity {
        val magic = ByteArray(Jhlog.FILE_MAGIC.size)
        input.readFullyExact(magic)
        val processRecording = magic.contentEquals(ProcessRecordingSession.MAGIC)
        if (processRecording) input.readFullyExact(magic)
        if (!magic.contentEquals(Jhlog.FILE_MAGIC)) throw IOException("Invalid JHLOG magic")
        val fixed = ByteArray(Int.SIZE_BYTES * 2)
        input.readFullyExact(fixed)
        val values = ByteBuffer.wrap(fixed).order(ByteOrder.LITTLE_ENDIAN)
        val payloadBytes = values.int
        val expectedCrc = values.int.toLong() and UINT_MASK
        if (payloadBytes !in MIN_IDENTITY_HEADER_BYTES..Jhlog.MAX_FILE_HEADER_BYTES) {
            throw IOException("Invalid JHLOG header size")
        }
        val payload = ByteArray(payloadBytes)
        input.readFullyExact(payload)
        val crc = CRC32().apply { update(payload) }.value
        if (crc != expectedCrc) throw IOException("Invalid JHLOG header CRC")
        val schema = readUvarint(payload, 0)
        if (schema.value != Jhlog.HEADER_SCHEMA) throw IOException("Unsupported JHLOG header schema")
        var offset = skipUvarint(payload, schema.nextOffset)
        offset = skipUvarint(payload, offset)
        if (offset + IDENTITY_BYTES > payload.size) throw IOException("Truncated JHLOG identity")
        val runId = payload.copyOfRange(offset, offset + ID_BYTES).toHex()
        offset += ID_BYTES
        val processInstanceId = payload.copyOfRange(offset, offset + ID_BYTES).toHex()
        offset += ID_BYTES * 2
        val segment = readUvarint(payload, offset)
        return JhlogIdentity(runId, processInstanceId, segment.value, processRecording)
    }

    private fun InputStream.readFullyExact(destination: ByteArray) {
        var offset = 0
        while (offset < destination.size) {
            val count = read(destination, offset, destination.size - offset)
            if (count < 0) throw IOException("Truncated JHLOG header")
            if (count > 0) offset += count
        }
    }

    private fun skipUvarint(bytes: ByteArray, offset: Int): Int = readUvarint(bytes, offset).nextOffset

    private fun readUvarint(bytes: ByteArray, start: Int): Uvarint {
        var value = 0L
        var shift = 0
        var offset = start
        while (offset < bytes.size && shift < Long.SIZE_BITS) {
            val byte = bytes[offset].toInt() and 0xff
            offset++
            if (shift == Long.SIZE_BITS - 1 && byte > 1) throw IOException("JHLOG header varint overflow")
            value = value or ((byte and 0x7f).toLong() shl shift)
            if (byte and 0x80 == 0) {
                if (byte == 0 && offset - start > 1) throw IOException("Non-canonical JHLOG header varint")
                return Uvarint(value, offset)
            }
            shift += 7
        }
        throw IOException("Invalid JHLOG header varint")
    }

    private fun ByteArray.toHex(): String {
        val chars = CharArray(size * 2)
        for (index in indices) {
            val value = this[index].toInt() and 0xff
            chars[index * 2] = HEX[value ushr 4]
            chars[index * 2 + 1] = HEX[value and 0x0f]
        }
        return String(chars)
    }

    fun interface FaultInjector {
        fun onPhase(phase: FaultPhase)

        companion object {
            val NONE = FaultInjector { }
        }
    }

    enum class FaultPhase { BEFORE_PUBLISH, AFTER_PUBLISH, AFTER_SOURCE_ARTIFACT_DELETE }

    data class Result(
        val archived: Long,
        val recovered: Long,
        val active: Long,
        val incomplete: Long,
    )

    private enum class ArchiveOutcome { ARCHIVED, RECOVERED, INCOMPLETE }
    private data class ArchiveSource(val file: File, val entryName: String, val size: Long, val crc: Long)
    private data class JhlogIdentity(
        val runId: String,
        val processInstanceId: String,
        val segmentIndex: Long,
        val processRecording: Boolean,
    )
    private data class Uvarint(val value: Long, val nextOffset: Int)

    private const val LOCK_FILE_NAME = ".jh-session-maintenance.lock"
    private const val ARCHIVE_SUFFIX = ".jhlog.zip"
    private const val TEMP_SUFFIX = ".jhlog.zip.tmp"
    private const val COPY_BUFFER_BYTES = 64 * 1024
    private const val MAX_PROCESS_DIRECTORIES = 1_024
    private const val MAX_ARTIFACTS_PER_PROCESS = 10_000
    private const val MAX_ARCHIVE_ENTRIES = 100_000
    private const val ID_BYTES = 16
    private const val IDENTITY_BYTES = ID_BYTES * 3
    private const val MIN_IDENTITY_HEADER_BYTES = IDENTITY_BYTES + 3
    private const val UINT_MASK = 0xffff_ffffL
    private val HEX = "0123456789abcdef".toCharArray()
}
