package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.FileInputStream
import java.io.DataInputStream
import java.io.IOException
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.MessageDigest
import java.util.zip.CRC32

/** Bounded whole-archive LRU with deterministic session-age recovery when its ledger is unavailable. */
internal object SessionArchiveRetention {
    internal const val LEDGER_FILE_NAME = ".jh-archive-lru.bin"

    fun enforce(
        root: File,
        budgetBytes: Long,
        protectedPaths: Set<String> = emptySet(),
        preserveNewest: Boolean = true,
    ): Result {
        require(budgetBytes >= 0L) { "archive retention budget must be non-negative" }
        if (!root.isDirectory) return Result.EMPTY
        return SessionArtifactReadLeases.mutate(root, Result.EMPTY) {
            CrossProcessFileLocks.withDirectoryLock(root, LOCK_FILE_NAME) {
                enforceLocked(root, budgetBytes, protectedPaths, preserveNewest)
            }
        }
    }

    fun recordAccess(root: File, archive: File) {
        if (!root.isDirectory || archive.parentFile?.absoluteFile != root.absoluteFile) return
        val parsed = parseArchive(archive) ?: return
        CrossProcessFileLocks.withDirectoryLock(root, LOCK_FILE_NAME) {
            val loaded = readLedger(root)
            val archives = scanArchives(root, loaded.entries.keys)
            if (archives.none { candidate -> candidate.file.name == parsed.file.name }) return@withDirectoryLock
            val rebuilt = reconcile(archives, loaded.entries, loaded.nextSequence)
            val next = nextSequence(rebuilt.nextSequence, archives, rebuilt.entries)
            rebuilt.entries[archiveKey(parsed.file.name)] = next
            writeLedger(root, next, rebuilt.entries)
        }
    }

    private fun enforceLocked(
        root: File,
        budgetBytes: Long,
        protectedPaths: Set<String>,
        preserveNewest: Boolean,
    ): Result {
        val loaded = readLedger(root)
        val archives = scanArchives(root, loaded.entries.keys)
        val ledger = reconcile(archives, loaded.entries, loaded.nextSequence)
        val protected = protectedPaths.mapTo(HashSet(protectedPaths.size)) { path -> File(path).absolutePath }
        var totalBytes = 0L
        for (archive in archives) totalBytes = saturatedAdd(totalBytes, archive.bytes)
        var deletedArchives = 0L
        var deletedBytes = 0L
        var failed = 0L
        if (budgetBytes < Long.MAX_VALUE && totalBytes > budgetBytes) {
            val candidates = archives.sortedWith(
                compareBy<Archive> { archive -> ledger.entries[archive.key] ?: 0L }
                    .thenBy(Archive::startedAtUnixMs)
                    .thenBy(Archive::dailySessionIndex)
                    .thenBy { archive -> archive.file.name },
            )
            val newestKey = if (preserveNewest && budgetBytes > 0L && protected.isEmpty()) {
                candidates.lastOrNull()?.key
            } else {
                null
            }
            for (archive in candidates) {
                if (totalBytes <= budgetBytes) break
                if (archive.key == newestKey || archive.file.absolutePath in protected) continue
                if (!SessionArchiveCoordinator.isPublishedArchive(archive.file)) {
                    ledger.entries.remove(archive.key)
                    totalBytes -= archive.bytes
                    failed++
                    continue
                }
                if (!archive.file.delete()) {
                    failed++
                    break
                }
                ledger.entries.remove(archive.key)
                totalBytes -= archive.bytes
                deletedBytes = saturatedAdd(deletedBytes, archive.bytes)
                deletedArchives++
            }
        }
        val ledgerFailed = runCatching {
            writeLedger(root, ledger.nextSequence, ledger.entries)
        }.isFailure
        return Result(
            deletedArchives = deletedArchives,
            deletedBytes = deletedBytes,
            remainingBytes = totalBytes,
            failed = failed,
            usedOldestFallback = loaded.corrupt || loaded.absent,
            ledgerWriteFailed = ledgerFailed,
        )
    }

    private fun scanArchives(root: File, trustedKeys: Set<String>): List<Archive> {
        val files = root.listFiles { file -> file.isFile }.orEmpty()
        val archives = ArrayList<Archive>(minOf(files.size, MAX_LEDGER_ENTRIES))
        for (file in files) {
            val archive = parseArchive(file) ?: continue
            if (archive.key !in trustedKeys && !SessionArchiveCoordinator.isPublishedArchive(file)) continue
            if (archives.size == MAX_LEDGER_ENTRIES) {
                throw IOException("Archive retention capacity exceeded")
            }
            archives += archive
        }
        archives.sortWith(
            compareBy(Archive::startedAtUnixMs)
                .thenBy(Archive::dailySessionIndex)
                .thenBy { archive -> archive.file.name },
        )
        return archives
    }

    private fun parseArchive(file: File): Archive? {
        if (!file.name.endsWith(ARCHIVE_SUFFIX)) return null
        val sessionName = file.name.removeSuffix(ARCHIVE_SUFFIX)
        val parsed = SessionArtifactPath.parseSessionDirectoryName(sessionName) ?: return null
        return Archive(
            file = file,
            key = archiveKey(file.name),
            bytes = file.length().coerceAtLeast(0L),
            startedAtUnixMs = parsed.startedAtUnixMs,
            dailySessionIndex = parsed.dailySessionIndex,
        )
    }

    private fun reconcile(
        archives: List<Archive>,
        loadedEntries: MutableMap<String, Long>,
        loadedNextSequence: Long,
    ): Ledger {
        val liveKeys = archives.mapTo(HashSet(archives.size), Archive::key)
        loadedEntries.keys.retainAll(liveKeys)
        var next = loadedNextSequence
        if (next == Long.MAX_VALUE) {
            loadedEntries.clear()
            next = 0L
        }
        for (archive in archives) {
            if (archive.key in loadedEntries) continue
            next++
            loadedEntries[archive.key] = next
        }
        return Ledger(loadedEntries, next)
    }

    private fun nextSequence(
        current: Long,
        archives: List<Archive>,
        entries: MutableMap<String, Long>,
    ): Long {
        if (current < Long.MAX_VALUE) return current + 1L
        entries.clear()
        var sequence = 0L
        for (archive in archives) entries[archive.key] = ++sequence
        return ++sequence
    }

    private fun readLedger(root: File): LoadedLedger {
        val file = File(root, LEDGER_FILE_NAME)
        if (!file.exists()) return LoadedLedger(HashMap(), 0L, absent = true, corrupt = false)
        return runCatching {
            val length = file.length()
            if (length !in MIN_LEDGER_BYTES.toLong()..MAX_LEDGER_BYTES.toLong()) {
                throw IOException("Invalid archive LRU ledger size")
            }
            val bytes = ByteArray(length.toInt())
            FileInputStream(file).use { raw ->
                DataInputStream(raw).readFully(bytes)
                if (raw.read() != -1) throw IOException("Archive LRU ledger changed during read")
            }
            val expectedCrc = ByteBuffer.wrap(bytes, bytes.size - Int.SIZE_BYTES, Int.SIZE_BYTES)
                .order(ByteOrder.LITTLE_ENDIAN).int.toLong() and UINT_MASK
            val actualCrc = CRC32().apply { update(bytes, 0, bytes.size - Int.SIZE_BYTES) }.value
            if (actualCrc != expectedCrc) throw IOException("Invalid archive LRU ledger CRC")
            val input = ByteBuffer.wrap(bytes).order(ByteOrder.LITTLE_ENDIAN)
            val magic = ByteArray(MAGIC.size).also(input::get)
            if (!magic.contentEquals(MAGIC) || input.int != SCHEMA) throw IOException("Invalid archive LRU ledger")
            val next = input.long
            val count = input.int
            if (next < 0L || count !in 0..MAX_LEDGER_ENTRIES || bytes.size != ledgerBytes(count)) {
                throw IOException("Invalid archive LRU ledger record count")
            }
            val entries = HashMap<String, Long>(count * 4 / 3 + 1)
            repeat(count) {
                val key = ByteArray(KEY_BYTES).also(input::get).toHex()
                val sequence = input.long
                if (sequence <= 0L || sequence > next || entries.put(key, sequence) != null) {
                    throw IOException("Invalid archive LRU ledger entry")
                }
            }
            LoadedLedger(entries, next, absent = false, corrupt = false)
        }.getOrElse { LoadedLedger(HashMap(), 0L, absent = false, corrupt = true) }
    }

    private fun writeLedger(root: File, nextSequence: Long, entries: Map<String, Long>) {
        if (entries.size > MAX_LEDGER_ENTRIES) throw IOException("Archive LRU ledger capacity exceeded")
        val ordered = entries.entries.sortedBy(Map.Entry<String, Long>::key)
        val bytes = ByteArray(ledgerBytes(ordered.size))
        val output = ByteBuffer.wrap(bytes).order(ByteOrder.LITTLE_ENDIAN)
        output.put(MAGIC).putInt(SCHEMA).putLong(nextSequence).putInt(ordered.size)
        for ((key, sequence) in ordered) output.put(key.hexToBytes()).putLong(sequence)
        output.putInt(CRC32().apply { update(bytes, 0, bytes.size - Int.SIZE_BYTES) }.value.toInt())
        val temporary = File(root, "$LEDGER_FILE_NAME.tmp")
        RandomAccessFile(temporary, "rw").use { file ->
            file.setLength(0L)
            file.write(bytes)
            file.fd.sync()
        }
        val target = File(root, LEDGER_FILE_NAME)
        if (target.exists() && !target.delete()) throw IOException("Cannot replace archive LRU ledger")
        if (!temporary.renameTo(target)) throw IOException("Cannot publish archive LRU ledger")
    }

    private fun archiveKey(name: String): String = MessageDigest.getInstance("SHA-256")
        .digest(name.toByteArray(Charsets.UTF_8))
        .toHex()

    private fun String.hexToBytes(): ByteArray {
        val bytes = ByteArray(length / 2)
        for (index in bytes.indices) bytes[index] = substring(index * 2, index * 2 + 2).toInt(16).toByte()
        return bytes
    }

    private fun ByteArray.toHex(): String {
        val chars = CharArray(size * 2)
        for (index in indices) {
            val value = this[index].toInt() and BYTE_MASK
            chars[index * 2] = HEX[value ushr 4]
            chars[index * 2 + 1] = HEX[value and 0x0f]
        }
        return String(chars)
    }

    private fun ledgerBytes(count: Int): Int = HEADER_BYTES + count * RECORD_BYTES + Int.SIZE_BYTES

    private fun saturatedAdd(first: Long, second: Long): Long =
        if (Long.MAX_VALUE - first < second) Long.MAX_VALUE else first + second

    data class Result(
        val deletedArchives: Long,
        val deletedBytes: Long,
        val remainingBytes: Long,
        val failed: Long,
        val usedOldestFallback: Boolean,
        val ledgerWriteFailed: Boolean,
    ) {
        companion object {
            val EMPTY = Result(0L, 0L, 0L, 0L, usedOldestFallback = false, ledgerWriteFailed = false)
        }
    }

    private data class Archive(
        val file: File,
        val key: String,
        val bytes: Long,
        val startedAtUnixMs: Long,
        val dailySessionIndex: Long,
    )
    private data class Ledger(val entries: MutableMap<String, Long>, val nextSequence: Long)
    private data class LoadedLedger(
        val entries: MutableMap<String, Long>,
        val nextSequence: Long,
        val absent: Boolean,
        val corrupt: Boolean,
    )

    private const val LOCK_FILE_NAME = ".jh-archive-retention.lock"
    private const val ARCHIVE_SUFFIX = ".jhlog.zip"
    private const val SCHEMA = 1
    private const val MAX_LEDGER_ENTRIES = 10_000
    private const val KEY_BYTES = 32
    private const val HEADER_BYTES = 4 + Int.SIZE_BYTES + Long.SIZE_BYTES + Int.SIZE_BYTES
    private const val RECORD_BYTES = KEY_BYTES + Long.SIZE_BYTES
    private const val MIN_LEDGER_BYTES = HEADER_BYTES + Int.SIZE_BYTES
    private const val MAX_LEDGER_BYTES = HEADER_BYTES + MAX_LEDGER_ENTRIES * RECORD_BYTES + Int.SIZE_BYTES
    private const val UINT_MASK = 0xffff_ffffL
    private const val BYTE_MASK = 0xff
    private val MAGIC = byteArrayOf('J'.code.toByte(), 'H'.code.toByte(), 'L'.code.toByte(), 'R'.code.toByte())
    private val HEX = "0123456789abcdef".toCharArray()
}
