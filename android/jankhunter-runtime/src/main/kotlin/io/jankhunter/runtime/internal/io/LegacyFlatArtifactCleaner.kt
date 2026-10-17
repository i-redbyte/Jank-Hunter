package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.system.RetainedHeapDumper
import java.io.File
import java.io.RandomAccessFile

/** Removes only legacy flat artifacts whose names and binary signatures prove SDK ownership. */
internal object LegacyFlatArtifactCleaner {
    fun clean(root: File): Result {
        if (!root.isDirectory) return Result.EMPTY
        return CrossProcessFileLocks.withDirectoryLock(root, LOCK_FILE_NAME) {
            cleanLocked(root)
        }
    }

    private fun cleanLocked(root: File): Result {
        val activeRunIds = ProcessRunCohort.activeRunIds(root)
        val protectedPaths = SessionLogAllocator.activeLeases(root).protectedPaths
        val magic = ByteArray(Jhlog.FILE_MAGIC.size)
        var deleted = 0L
        var failed = 0L
        var protected = 0L
        for (file in root.listFiles { candidate -> candidate.isFile }.orEmpty()) {
            val parsed = SessionLogName.parse(file.name)
            val managedJhlog = parsed != null && hasManagedJhlogMagic(file, magic)
            val managedHeap = RetainedHeapDumper.isManagedHeapDumpFileName(file.name)
            if (!managedJhlog && !managedHeap) continue
            val isProtected = file.absolutePath in protectedPaths ||
                file.name in protectedPaths ||
                parsed?.runId in activeRunIds ||
                managedHeap && activeRunIds.isNotEmpty()
            if (isProtected) {
                protected++
            } else if (file.delete()) {
                deleted++
            } else {
                failed++
            }
        }
        return Result(deleted, failed, protected)
    }

    private fun hasManagedJhlogMagic(file: File, buffer: ByteArray): Boolean = runCatching {
        if (file.length() < buffer.size) return@runCatching false
        RandomAccessFile(file, "r").use { input -> input.readFully(buffer) }
        val versionOffset = buffer.size - VERSION_BYTES
        for (index in 0 until versionOffset) {
            if (buffer[index] != Jhlog.FILE_MAGIC[index]) return@runCatching false
        }
        val major = buffer[versionOffset].toInt() and BYTE_MASK
        val minor = buffer[versionOffset + 1].toInt() and BYTE_MASK
        val patch = buffer[versionOffset + 2].toInt() and BYTE_MASK
        val currentMajor = Jhlog.FILE_MAGIC[versionOffset].toInt() and BYTE_MASK
        val currentMinor = Jhlog.FILE_MAGIC[versionOffset + 1].toInt() and BYTE_MASK
        val currentPatch = Jhlog.FILE_MAGIC[versionOffset + 2].toInt() and BYTE_MASK
        major > 0 && (
            major < currentMajor ||
                major == currentMajor && minor < currentMinor ||
                major == currentMajor && minor == currentMinor && patch <= currentPatch
            )
    }.getOrDefault(false)

    data class Result(val deleted: Long, val failed: Long, val protected: Long) {
        companion object {
            val EMPTY = Result(0L, 0L, 0L)
        }
    }

    private const val LOCK_FILE_NAME = ".jh-legacy-flat-cleanup.lock"
    private const val VERSION_BYTES = 3
    private const val BYTE_MASK = 0xff
}
