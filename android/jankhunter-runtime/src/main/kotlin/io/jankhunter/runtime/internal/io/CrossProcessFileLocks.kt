package io.jankhunter.runtime.internal.io

import java.io.Closeable
import java.io.File
import java.io.RandomAccessFile

/** Serializes short metadata transactions both inside one JVM and across application processes. */
internal object CrossProcessFileLocks {
    private val registryLock = Any()
    private val processLocks = HashMap<String, ProcessLock>()
    private val heldLockKeys = ThreadLocal<MutableSet<String>>()

    fun <T> withDirectoryLock(directory: File, lockFileName: String, block: () -> T): T {
        val key = lockKey(directory, lockFileName)
        val processLock = retain(key)
        try {
            return synchronized(processLock.monitor) {
                val held = heldLockKeys.get() ?: HashSet<String>(2).also(heldLockKeys::set)
                if (!held.add(key)) return@synchronized block()
                try {
                    RandomAccessFile(File(directory, lockFileName), "rw").use { access ->
                        access.channel.lock().use { block() }
                    }
                } finally {
                    held.remove(key)
                    if (held.isEmpty()) heldLockKeys.remove()
                }
            }
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
        val monitor = Any()
        var references = 0
    }

    internal class ProcessLockHandle(
        private val key: String,
        private val lock: ProcessLock,
    ) : Closeable {
        val monitor: Any
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
