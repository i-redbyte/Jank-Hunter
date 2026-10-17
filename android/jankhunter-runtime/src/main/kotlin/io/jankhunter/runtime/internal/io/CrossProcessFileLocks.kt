package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.RandomAccessFile
import java.io.IOException
import java.nio.channels.OverlappingFileLockException
import java.util.concurrent.locks.ReentrantLock

/** Serializes short metadata transactions both inside one JVM and across application processes. */
internal object CrossProcessFileLocks {
    private val registryLock = Any()
    private val processLocks = HashMap<String, ProcessLock>()
    private val heldLockKeys = ThreadLocal<MutableSet<String>>()
    private val nonblocking = ThreadLocal<Boolean>()

    /** Startup may fail closed with diagnostics instead of waiting for another process. */
    fun <T> withoutLockWait(block: () -> T): T {
        val previous = nonblocking.get()
        nonblocking.set(true)
        try { return block() } finally {
            if (previous == null) nonblocking.remove() else nonblocking.set(previous)
        }
    }

    fun <T> withDirectoryLock(directory: File, lockFileName: String, block: () -> T): T {
        val key = lockKey(directory, lockFileName)
        val processLock = retain(key)
        try {
            val mutex = processLock.monitor
            if (nonblocking.get() == true) {
                if (!mutex.tryLock()) throw IOException("Jank Hunter directory transaction is busy")
            } else mutex.lock()
            try {
                val held = heldLockKeys.get() ?: HashSet<String>(2).also(heldLockKeys::set)
                if (!held.add(key)) return block()
                try {
                    RandomAccessFile(File(directory, lockFileName), "rw").use { access ->
                        val lock = if (nonblocking.get() == true) {
                            try { access.channel.tryLock() } catch (_: OverlappingFileLockException) { null }
                                ?: throw IOException("Jank Hunter directory file lock is busy")
                        } else access.channel.lock()
                        return lock.use { block() }
                    }
                } finally {
                    held.remove(key)
                    if (held.isEmpty()) heldLockKeys.remove()
                }
            } finally { mutex.unlock() }
        } finally {
            release(key, processLock)
        }
    }

    fun acquireProcessLock(directory: File, scope: String): ProcessLockHandle {
        val key = lockKey(directory, scope)
        return ProcessLockHandle(key, retain(key))
    }

    fun fileKey(file: File): String =
        runCatching { file.canonicalPath }.getOrElse { file.absolutePath }

    private fun retain(key: String): ProcessLock = synchronized(registryLock) {
        val lock = processLocks[key] ?: ProcessLock().also { processLocks[key] = it }
        lock.references++
        lock
    }

    private fun release(key: String, lock: ProcessLock) = synchronized(registryLock) {
        check(processLocks[key] === lock && lock.references > 0) { "process lock ownership mismatch" }
        lock.references--
        if (lock.references == 0) processLocks.remove(key)
    }

    private fun lockKey(directory: File, scope: String): String = "${fileKey(directory)}\u0000$scope"

    internal class ProcessLock {
        val monitor = ReentrantLock()
        var references = 0
    }

    internal class ProcessLockHandle(
        private val key: String,
        private val lock: ProcessLock,
    ) : Closeable {
        val monitor: ReentrantLock
            get() = lock.monitor

        private var closed = false

        @Synchronized
        override fun close() {
            if (closed) return
            closed = true
            CrossProcessFileLocks.release(key, lock)
        }
    }
}
