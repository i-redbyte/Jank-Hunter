package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterBinaryArtifact
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterBinaryWriter
import java.io.Closeable
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/** Process-lifetime ownership, independent of collector restarts and snapshot boundaries. */
internal class ProcessRecordingSession(
    private val processIdentity: ByteArray = ProcessInstanceIdentity.id(),
) : Closeable {
    fun processInstanceId(): ByteArray = processIdentity.copyOf()

    fun resolveDirectory(requested: File): File = ownerLock.withLock {
        root ?: requested.absoluteFile.also { root = it }
    }

    private var root: File? = null
    private var lease: ProcessRunCohort.Lease? = null
    private var allocation: SessionLogAllocator.Allocation? = null
    private var protection: JankHunterBinaryArtifact? = null
    private var storage: JankHunterBinaryStorage? = null
    private var path: String? = null
    private val ownerLock = ReentrantLock()
    private val ownerReleased = ownerLock.newCondition()
    private var state = EpochState.READY
    private var opened = false
    private var ledger: SessionSegmentLedger? = null
    private var closeWhenSealed = false

    /** Releases process ownership after the active writer publishes its sealed frontier. */
    fun releaseAfterEpoch() = ownerLock.withLock {
        if (state == EpochState.WRITING) closeWhenSealed = true else closeOwnedResources()
    }

    fun join(directory: File, localDate: String, startedAtMs: Long): ProcessRunCohort.Lease = ownerLock.withLock {
        val existing = lease
        if (existing != null) {
            if (root == directory.absoluteFile) return@withLock existing
            while (state == EpochState.WRITING) ownerReleased.await()
        }
        ProcessRunCohort.join(directory, localDate, startedAtUnixMs = startedAtMs).also {
            if (existing != null) close()
            root = directory.absoluteFile
            lease = it
        }
    }

    fun ledger(processDirectory: File, processName: String, coordinationDirectory: File): SessionSegmentLedger =
        ownerLock.withLock {
            ledger ?: SessionSegmentLedger(processDirectory, processName, coordinationDirectory).also { ledger = it }
        }

    fun openEpoch(
        directory: File,
        processDirectory: File,
        localDate: String,
        runId: ByteArray,
        dailySessionIndex: Long,
        target: JankHunterBinaryStorage?,
        physicalLimit: Long,
        beforeFirstWrite: () -> Unit,
        createWriter: (JankHunterBinaryWriter, Long) -> BinaryLogWriter,
    ): OpenedLogSession = ownerLock.withLock {
        while (state == EpochState.WRITING) ownerReleased.await()
        if (state == EpochState.FAILED) throw IOException("Cannot append after an unsealed Jank Hunter epoch")
        val owned = allocation ?: SessionLogAllocator.reserve(
            directory, localDate, runId, dailySessionIndex, target?.listFiles(),
        ).also { allocation = it }
        val previousPath = path
        val previousBytes = previousPath?.let { File(it).length() } ?: 0L
        val output = if (target == null) {
            if (!processDirectory.isDirectory && !processDirectory.mkdirs()) {
                throw IOException("Cannot create process directory: $processDirectory")
            }
            val file = File(processDirectory, owned.fileName)
            if (!opened && !file.createNewFile()) throw IOException("Process recording name collision: $file")
            if (opened && !file.isFile) throw IOException("Process recording disappeared: $file")
            AppendFileWriter(file)
        } else {
            target.openWriter(owned.fileName)
        }
        var attemptedWrite = false
        try {
            if (output.bytesWritten() != previousBytes) throw IOException("Process recording length changed")
            if (physicalLimit > 0L && physicalLimit - previousBytes < MAGIC.size) {
                throw LogSizeLimitReachedException("Process recording limit reached")
            }
            attemptedWrite = true
            if (previousBytes == 0L) {
                beforeFirstWrite()
                output.writeBytes(MAGIC)
                output.flush()
            }
            val start = output.bytesWritten()
            val remaining = if (physicalLimit > 0L) physicalLimit - start else 0L
            if (physicalLimit > 0L && remaining <= 0L) throw LogSizeLimitReachedException("Process recording limit reached")
            owned.updateProtectedPath(output.path)
            val activeProtection = if (target === storage && opened) protection else target?.protect(owned.fileName)
            val writer = try {
                state = EpochState.WRITING
                createWriter(EpochWriter(output, start), remaining)
            } catch (error: Throwable) {
                if (activeProtection !== protection) runCatching { activeProtection?.commit() }
                throw error
            }
            path = output.path
            storage = target
            protection = activeProtection
            opened = true
            return@withLock OpenedLogSession(owned, writer, activeProtection)
        } catch (error: Throwable) {
            runCatching { output.close() }
            if (attemptedWrite) {
                // A failed constructor has admitted no events; restore the previous sealed frontier.
                state = if (runCatching {
                    java.io.RandomAccessFile(output.path, "rw").use { it.setLength(previousBytes) }
                    if (!opened) {
                        if (target == null) File(output.path).delete() else target.delete(owned.fileName)
                    }
                }.isSuccess) EpochState.READY else EpochState.FAILED
            }
            ownerReleased.signalAll()
            throw error
        }
    }

    fun adoptProtection(active: JankHunterBinaryArtifact?) = ownerLock.withLock {
        protection = active
    }

    fun completeEpoch(writer: BinaryLogWriter) = ownerLock.withLock {
        state = if (writer.sealedDigest() != null) EpochState.READY else EpochState.FAILED
        if (closeWhenSealed) closeOwnedResources()
        ownerReleased.signalAll()
    }

    override fun close() = ownerLock.withLock {
        closeOwnedResources()
    }

    private fun closeOwnedResources() {
        try {
            try {
                ledger?.finish()
            } finally {
                try {
                    allocation?.close()
                } finally {
                    lease?.close()
                }
            }
        } finally {
            ledger = null
            allocation = null
            lease = null
            root = null
            path = null
            protection = null
            storage = null
            opened = false
            closeWhenSealed = false
            state = EpochState.READY
            ownerReleased.signalAll()
        }
    }

    private enum class EpochState { READY, WRITING, FAILED }

    private class EpochWriter(private val delegate: JankHunterBinaryWriter, private val start: Long) : JankHunterBinaryWriter {
        override val path: String get() = delegate.path
        override fun bytesWritten(): Long = delegate.bytesWritten() - start
        override fun writeByte(byte: Byte) = delegate.writeByte(byte)
        override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) = delegate.writeBytes(bytes, offset, length)
        override fun flush() = delegate.flush()
        override fun close() = delegate.close()
    }

    private class AppendFileWriter(file: File) : JankHunterBinaryWriter {
        override val path: String = file.absolutePath
        private var written = file.length()
        private val output = FileOutputStream(file, true)
        override fun bytesWritten(): Long = written
        override fun writeByte(byte: Byte) {
            output.write(byte.toInt())
            written++
        }
        override fun writeBytes(bytes: ByteArray, offset: Int, length: Int) {
            output.write(bytes, offset, length)
            written += length
        }
        override fun flush() = output.flush()
        override fun close() = output.close()
    }

    companion object {
        val MAGIC = byteArrayOf(0x4a, 0x48, 0x4c, 0x4f, 0x47, 0x0d, 0x0a, 0x82.toByte(), 1, 0, 0)
    }
}
