package io.jankhunter.gradle

import java.io.File
import java.io.RandomAccessFile

internal object InstrumentationArtifactFiles {
    fun prepare(path: String) {
        if (path.isBlank()) return
        val file = File(path)
        withLock(file) {
            file.delete()
        }
    }

    fun append(path: String, text: String) {
        if (path.isBlank() || text.isEmpty()) return
        val file = File(path)
        withLock(file) {
            file.appendText(text)
        }
    }

    private inline fun withLock(file: File, block: () -> Unit) {
        file.parentFile?.mkdirs()
        val lockFile = File(file.parentFile ?: File("."), "${file.name}.lock")
        RandomAccessFile(lockFile, "rw").channel.use { channel ->
            channel.lock().use {
                block()
            }
        }
    }
}
