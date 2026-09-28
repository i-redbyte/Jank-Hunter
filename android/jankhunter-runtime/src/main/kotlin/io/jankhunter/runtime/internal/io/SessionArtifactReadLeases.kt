package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.channels.FileLock
import java.nio.channels.OverlappingFileLockException

/** Cold export leases keep source artifacts alive without holding a lock while streaming them. */
internal object SessionArtifactReadLeases {
    private const val DIRECTORY = ".jh-export-readers"
    private const val MUTATION_LOCK = ".jh-artifact-mutation.lock"
    private const val SUFFIX = ".lease"
    private val emptyLease = Closeable { }

    fun acquire(root: File): Closeable {
        if (!root.isDirectory) return emptyLease
        return CrossProcessFileLocks.withDirectoryLock(root, MUTATION_LOCK) {
            val directory = File(root, DIRECTORY)
            if (!directory.isDirectory && !directory.mkdirs()) throw IOException("Cannot create export lease directory")
            val identity = SessionLogName.runIdHex(ProcessInstanceIdentity.id())
            val nonce = SessionLogName.runIdHex(BinaryLogFileHeader.randomId())
            val file = File(directory, "$identity-$nonce$SUFFIX")
            if (!file.createNewFile()) throw IOException("Cannot allocate export read lease")
            val access = RandomAccessFile(file, "rw")
            try {
                Lease(file, access, access.channel.lock())
            } catch (error: Throwable) {
                runCatching { access.close() }
                file.delete()
                throw error
            }
        }
    }

    /** Publishes a new immutable artifact; readers already hold their captured frontier. */
    fun <T> publish(root: File, action: () -> T): T =
        CrossProcessFileLocks.withDirectoryLock(root, MUTATION_LOCK, action)

    fun <T> mutate(root: File, blocked: T, action: () -> T): T {
        if (!root.isDirectory) return action()
        return CrossProcessFileLocks.withDirectoryLock(root, MUTATION_LOCK) {
            if (hasReaders(root)) blocked else action()
        }
    }

    private fun hasReaders(root: File): Boolean {
        val files = File(root, DIRECTORY).listFiles().orEmpty()
        val ownedPrefix = "${SessionLogName.runIdHex(ProcessInstanceIdentity.id())}-"
        for (file in files) {
            if (!file.isFile || !file.name.endsWith(SUFFIX)) continue
            // Never reopen this process's lock file: closing another fd can release POSIX locks.
            if (file.name.startsWith(ownedPrefix)) return true
            try {
                RandomAccessFile(file, "rw").use { access ->
                    val lock = try {
                        access.channel.tryLock()
                    } catch (_: OverlappingFileLockException) {
                        return true
                    }
                    if (lock == null) return true
                    lock.release()
                }
                file.delete()
            } catch (_: IOException) {
                return true
            } catch (_: SecurityException) {
                return true
            }
        }
        return false
    }

    private class Lease(
        private val file: File,
        private val access: RandomAccessFile,
        private val lock: FileLock,
    ) : Closeable {
        private var closed = false

        @Synchronized
        override fun close() {
            if (closed) return
            closed = true
            try {
                lock.release()
            } finally {
                try { access.close() } finally { file.delete() }
            }
        }
    }
}
