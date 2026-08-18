package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.util.concurrent.ConcurrentHashMap

/** Serializes short metadata transactions both inside one JVM and across application processes. */
internal object CrossProcessFileLocks {
    private val processLocks = ConcurrentHashMap<String, Any>()
    private val heldLockKeys = ThreadLocal<MutableSet<String>>()

    fun <T> withDirectoryLock(directory: File, lockFileName: String, block: () -> T): T {
        val key = lockKey(directory, lockFileName)
        val processLock = processLocks.getOrPut(key) { Any() }
        return synchronized(processLock) {
            val held = heldLockKeys.get() ?: hashSetOf<String>().also(heldLockKeys::set)
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
    }

    fun processLock(directory: File, scope: String): Any =
        processLocks.getOrPut(lockKey(directory, scope)) { Any() }

    fun fileKey(file: File): String =
        runCatching { file.canonicalPath }.getOrElse { file.absolutePath }

    private fun lockKey(directory: File, scope: String): String = "${fileKey(directory)}\u0000$scope"
}
