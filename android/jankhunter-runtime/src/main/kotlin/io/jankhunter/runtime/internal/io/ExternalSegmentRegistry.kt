package io.jankhunter.runtime.internal.io

import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.nio.ByteBuffer
import java.security.MessageDigest
import java.util.zip.CRC32

/** Crash-safe process-scoped registry of `.jhlog` segments owned by external storage. */
internal class ExternalSegmentRegistry(
    private val directory: File,
    processName: String,
) {
    private val scope = processScope(processName)
    private val lockFileName = ".jh-external-segments-$scope.lock"
    private val slots = arrayOf(
        File(directory, ".jh-external-segments-$scope-0.bin"),
        File(directory, ".jh-external-segments-$scope-1.bin"),
    )

    fun paths(): List<String> = locked { readLatest().paths.toList() }

    fun add(path: String) {
        update { paths -> paths.add(normalize(path)) }
    }

    fun remove(path: String) {
        update { paths -> paths.remove(normalize(path)) }
    }

    private fun update(change: (LinkedHashSet<String>) -> Boolean) {
        locked {
            val current = readLatest()
            val updated = LinkedHashSet(current.paths)
            if (!change(updated)) return@locked
            if (updated.size > MAX_PATHS) throw IOException("Jank Hunter external segment registry is full")
            write(nextSlot(current.slot), nextSequence(current.sequence), updated)
        }
    }

    private fun <T> locked(block: () -> T): T {
        if (!directory.isDirectory && !directory.mkdirs()) {
            throw IOException("Cannot create Jank Hunter external segment registry directory: $directory")
        }
        return CrossProcessFileLocks.withDirectoryLock(directory, lockFileName, block)
    }

    private fun readLatest(): Snapshot {
        val first = read(slots[0], 0)
        val second = read(slots[1], 1)
        return when {
            first == null -> second ?: Snapshot(-1L, -1, linkedSetOf())
            second == null -> first
            first.sequence >= second.sequence -> first
            else -> second
        }
    }

    private fun read(file: File, slot: Int): Snapshot? {
        if (!file.isFile) return null
        return runCatching {
            val bytes = file.readBytes()
            if (bytes.size < MIN_RECORD_BYTES || bytes.size > MAX_REGISTRY_BYTES) return null
            val payloadBytes = bytes.size - Long.SIZE_BYTES
            val expectedCrc = ByteBuffer.wrap(bytes, payloadBytes, Long.SIZE_BYTES).long
            val crc = CRC32().apply { update(bytes, 0, payloadBytes) }.value
            if (crc != expectedCrc) return null
            DataInputStream(ByteArrayInputStream(bytes, 0, payloadBytes)).use { input ->
                if (input.readInt() != MAGIC || input.readInt() != VERSION) return null
                val sequence = input.readLong()
                val count = input.readInt()
                if (sequence < 0L || count !in 0..MAX_PATHS) return null
                val paths = LinkedHashSet<String>(mapCapacity(count))
                repeat(count) {
                    val length = input.readInt()
                    if (length !in 1..MAX_PATH_BYTES || length > input.available()) return null
                    val raw = ByteArray(length)
                    input.readFully(raw)
                    paths += normalize(String(raw, Charsets.UTF_8))
                }
                if (input.available() != 0) return null
                Snapshot(sequence, slot, paths)
            }
        }.getOrNull()
    }

    private fun write(slot: Int, sequence: Long, paths: Set<String>) {
        val payload = ByteArrayOutputStream(INITIAL_BUFFER_BYTES)
        DataOutputStream(payload).use { output ->
            output.writeInt(MAGIC)
            output.writeInt(VERSION)
            output.writeLong(sequence)
            output.writeInt(paths.size)
            paths.forEach { path ->
                val raw = path.toByteArray(Charsets.UTF_8)
                if (raw.isEmpty() || raw.size > MAX_PATH_BYTES) {
                    throw IOException("Jank Hunter external segment path is too long")
                }
                output.writeInt(raw.size)
                output.write(raw)
            }
        }
        val rawPayload = payload.toByteArray()
        if (rawPayload.size + Long.SIZE_BYTES > MAX_REGISTRY_BYTES) {
            throw IOException("Jank Hunter external segment registry exceeds its size limit")
        }
        val crc = CRC32().apply { update(rawPayload) }.value
        FileOutputStream(slots[slot], false).use { output ->
            output.write(rawPayload)
            output.write(ByteBuffer.allocate(Long.SIZE_BYTES).putLong(crc).array())
            output.flush()
            output.fd.sync()
        }
    }

    private fun nextSlot(current: Int): Int = if (current == 0) 1 else 0

    private fun nextSequence(current: Long): Long {
        if (current == Long.MAX_VALUE) throw IOException("Jank Hunter external segment registry sequence exhausted")
        return current + 1L
    }

    private fun normalize(path: String): String = File(path).absolutePath

    private data class Snapshot(
        val sequence: Long,
        val slot: Int,
        val paths: LinkedHashSet<String>,
    )

    private companion object {
        const val MAGIC = 0x4a485352
        const val VERSION = 1
        const val MAX_PATHS = 4_096
        const val MAX_PATH_BYTES = 4_096
        const val MAX_REGISTRY_BYTES = 16 * 1024 * 1024
        const val INITIAL_BUFFER_BYTES = 4 * 1024
        const val MIN_RECORD_BYTES = Int.SIZE_BYTES * 3 + Long.SIZE_BYTES * 2

        fun processScope(processName: String): String {
            val digest = MessageDigest.getInstance("SHA-256")
                .digest(processName.toByteArray(Charsets.UTF_8))
            val out = CharArray(PROCESS_SCOPE_HEX_CHARS)
            repeat(PROCESS_SCOPE_BYTES) { index ->
                val value = digest[index].toInt() and 0xff
                out[index * 2] = HEX[value ushr 4]
                out[index * 2 + 1] = HEX[value and 0x0f]
            }
            return out.concatToString()
        }

        fun mapCapacity(size: Int): Int = if (size < 3) size + 1 else size + size / 3 + 1

        const val PROCESS_SCOPE_BYTES = 8
        const val PROCESS_SCOPE_HEX_CHARS = PROCESS_SCOPE_BYTES * 2
        const val HEX = "0123456789abcdef"
    }
}
