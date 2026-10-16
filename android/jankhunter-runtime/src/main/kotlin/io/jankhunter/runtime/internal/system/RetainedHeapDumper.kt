package io.jankhunter.runtime.internal.system

import android.os.Debug
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.JankHunterStoragePolicy
import io.jankhunter.runtime.RuntimeLongSource
import io.jankhunter.runtime.RuntimeHookGuard
import io.jankhunter.runtime.internal.io.SessionStorageBudget
import io.jankhunter.runtime.internal.io.SessionArtifactReadLeases
import io.jankhunter.runtime.internal.io.SessionArtifactPath
import java.io.File
import java.io.IOException
import java.io.RandomAccessFile
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong

internal class RetainedHeapDumper(
    private val directory: File,
    binaryStorage: JankHunterBinaryStorage? = null,
    private val minIntervalMs: Long,
    maxDumpCount: Int,
    minRetainedAgeMs: Long = 0L,
    private val clock: RuntimeLongSource = RuntimeLongSource { android.os.SystemClock.elapsedRealtime() },
    private val wallClock: RuntimeLongSource = RuntimeLongSource { System.currentTimeMillis() },
    private val dumpHprof: (String) -> Unit = { path -> Debug.dumpHprofData(path) },
    private val managedDirectoryProvider: (() -> File)? = null,
    private val storagePolicy: JankHunterStoragePolicy? = null,
    private val storageRoot: File = directory,
) {
    @Volatile
    private var binaryStorage = binaryStorage
    private val maxCount = maxDumpCount.coerceAtLeast(0)
    private val minAgeMs = minRetainedAgeMs.coerceAtLeast(0L)
    private val lastDumpAtMs = AtomicLong(Long.MIN_VALUE)
    private val dumpCount = AtomicInteger()

    fun maybeDump(className: String?, holder: String?, ageMs: Long, count: Long): Result {
        if (storagePolicy?.allowsExtension("hprof") == false) return Result.Skipped("storage_extension")
        if (ageMs < minAgeMs) {
            return Result.Skipped("min_age")
        }
        if (maxCount <= 0) {
            return Result.Skipped("max_count")
        }
        val currentCount = dumpCount.get()
        if (currentCount >= maxCount) {
            return Result.Skipped("max_count")
        }
        val now = clock.getAsLong()
        val last = lastDumpAtMs.get()
        if (last != Long.MIN_VALUE && now - last < minIntervalMs) {
            return Result.Skipped("min_interval")
        }
        if (!lastDumpAtMs.compareAndSet(last, now)) {
            return Result.Skipped("concurrent")
        }
        val nextCount = dumpCount.incrementAndGet()
        if (nextCount > maxCount) {
            dumpCount.decrementAndGet()
            lastDumpAtMs.compareAndSet(now, last)
            return Result.Skipped("max_count")
        }
        return try {
            val fileName = "retained-${wallClock.getAsLong()}-${safeName(className)}-${nextCount}.hprof"
            val storage = binaryStorage
            val managedDirectory = if (storage == null) managedDirectoryProvider?.invoke() ?: directory else directory
            val file = dumpToFile(fileName, storage, managedDirectory)
            if (!enforceRetention(storage, file, managedDirectory)) {
                deleteDump(storage, file)
                throw RetentionCleanupException()
            }
            Result.Dumped(file, safeName(className), safeName(holder), ageMs, count)
        } catch (error: Throwable) {
            dumpCount.decrementAndGet()
            // Avoid repeatedly pausing ART for a dump that the host storage budget cannot admit.
            if (error !is HeapAdmissionException) lastDumpAtMs.compareAndSet(now, last)
            RuntimeHookGuard.rethrowFatal(error)
            if (error is HeapAdmissionException) Result.Skipped("storage_limit")
            else Result.Failed(error.javaClass.simpleName ?: "error")
        }
    }

    private fun dumpToFile(fileName: String, storage: JankHunterBinaryStorage?, managedDirectory: File): File {
        if (storage == null) {
            managedDirectory.mkdirs()
            val file = File(managedDirectory, fileName)
            val pending = File.createTempFile(".jh-heap-", ".pending", managedDirectory)
            return try {
                dumpHprof(pending.absolutePath)
                if (pending.length() <= 0L) throw IOException("Heap dump writer produced an empty file")
                RandomAccessFile(pending, "rw").use { it.fd.sync() }
                val policy = storagePolicy
                if (policy != null) {
                    if (!SessionStorageBudget.publishHeapDump(storageRoot, policy, pending, file)) {
                        throw HeapAdmissionException()
                    }
                } else if (file.exists() || !pending.renameTo(file)) {
                    throw IOException("Cannot publish completed Jank Hunter heap dump")
                }
                file
            } finally {
                pending.delete()
            }
        }

        val artifact = storage.createArtifact(fileName)
        var committed = false
        try {
            val file = File(artifact.path)
            dumpHprof(file.absolutePath)
            artifact.commit()
            committed = true
            return file
        } finally {
            if (!committed) {
                RuntimeHookGuard.swallow { artifact.abort() }
            }
        }
    }

    private fun enforceRetention(
        storage: JankHunterBinaryStorage?,
        newest: File,
        managedDirectory: File,
    ): Boolean {
        val session = managedDirectory.parentFile
        val root = session?.parentFile?.takeIf {
            SessionArtifactPath.isCanonicalId(managedDirectory.name) &&
                SessionArtifactPath.parseSessionDirectoryName(session.name) != null
        }
        return if (storage == null && storagePolicy == null && root != null) {
            SessionArtifactReadLeases.mutate(root, blocked = true) {
                enforceRetentionUnlocked(storage, newest, managedDirectory)
            }
        } else {
            enforceRetentionUnlocked(storage, newest, managedDirectory)
        }
    }

    private fun enforceRetentionUnlocked(
        storage: JankHunterBinaryStorage?,
        newest: File,
        managedDirectory: File,
    ): Boolean {
        val paths = try {
            if (storage == null) {
                managedDirectory.listFiles { file -> file.isFile }.orEmpty().map(File::getAbsolutePath)
            } else {
                storage.listFiles()
            }
        } catch (error: Throwable) {
            RuntimeHookGuard.rethrowFatal(error)
            return false
        }
        val dumps = ArrayList<ManagedHeapDump>(paths.size)
        for (path in paths) {
            managedHeapDump(path)
                ?.takeUnless { dump -> dump.name == newest.name }
                ?.let(dumps::add)
        }
        val retainedPreviousCount = maxCount - 1
        if (dumps.size <= retainedPreviousCount) return true
        dumps.sortWith(MANAGED_DUMP_NEWEST_FIRST)
        for (index in retainedPreviousCount until dumps.size) {
            if (!deleteDump(storage, File(dumps[index].path))) return false
        }
        return true
    }

    private fun deleteDump(storage: JankHunterBinaryStorage?, file: File): Boolean {
        return try {
            if (storage == null) {
                if (storagePolicy != null) SessionStorageBudget.deleteHeapDump(storageRoot, file)
                else !file.exists() || file.delete()
            } else {
                storage.delete(file.name)
                storage.listFiles().none { path -> File(path).name == file.name }
            }
        } catch (error: Throwable) {
            RuntimeHookGuard.rethrowFatal(error)
            false
        }
    }

    fun switchBinaryStorage(storage: JankHunterBinaryStorage?) {
        binaryStorage = storage
    }

    sealed class Result {
        data class Dumped(
            val file: File,
            val className: String,
            val holder: String,
            val ageMs: Long,
            val count: Long,
        ) : Result()

        data class Skipped(val reason: String) : Result()

        data class Failed(val reason: String) : Result()
    }

    companion object {
        private const val MANAGED_DUMP_PREFIX = "retained-"
        private const val MANAGED_DUMP_SUFFIX = ".hprof"
        private val MANAGED_DUMP_NEWEST_FIRST = compareByDescending<ManagedHeapDump> { dump -> dump.timestamp }
            .thenByDescending { dump -> dump.sequence }
            .thenByDescending { dump -> dump.name }

        internal fun safeName(value: String?): String {
            val normalized = value
                ?.trim()
                ?.takeIf { it.isNotEmpty() }
                ?: "unknown"
            val out = StringBuilder(normalized.length)
            for (char in normalized) {
                out.append(
                    when {
                        char.isLetterOrDigit() || char == '.' || char == '_' || char == '-' -> char
                        else -> '_'
                    },
                )
            }
            return out.toString().take(96).ifEmpty { "unknown" }
        }

        internal fun isManagedHeapDumpFileName(fileName: String): Boolean = managedHeapDump(fileName) != null

        private fun managedHeapDump(path: String): ManagedHeapDump? {
            val name = File(path).name
            if (!name.startsWith(MANAGED_DUMP_PREFIX) || !name.endsWith(MANAGED_DUMP_SUFFIX)) return null
            val contentEnd = name.length - MANAGED_DUMP_SUFFIX.length
            val timestampEnd = name.indexOf('-', MANAGED_DUMP_PREFIX.length)
            val sequenceStart = name.lastIndexOf('-', contentEnd - 1) + 1
            if (timestampEnd <= MANAGED_DUMP_PREFIX.length || sequenceStart <= timestampEnd + 1 || sequenceStart >= contentEnd) {
                return null
            }
            val timestamp = asciiLong(name, MANAGED_DUMP_PREFIX.length, timestampEnd) ?: return null
            val sequence = asciiLong(name, sequenceStart, contentEnd) ?: return null
            val safeNameStart = timestampEnd + 1
            val safeNameEnd = sequenceStart - 1
            if (safeNameEnd - safeNameStart !in 1..96) return null
            for (index in safeNameStart until safeNameEnd) {
                val char = name[index]
                if (!char.isLetterOrDigit() && char != '.' && char != '_' && char != '-') return null
            }
            return ManagedHeapDump(path, name, timestamp, sequence)
        }

        private fun asciiLong(value: String, start: Int, end: Int): Long? {
            var result = 0L
            for (index in start until end) {
                val digit = value[index].code - '0'.code
                if (digit !in 0..9 || result > (Long.MAX_VALUE - digit) / 10L) return null
                result = result * 10L + digit
            }
            return result
        }
    }

    private data class ManagedHeapDump(
        val path: String,
        val name: String,
        val timestamp: Long,
        val sequence: Long,
    )

    private class RetentionCleanupException : IllegalStateException("managed HPROF retention cleanup failed")
    private class HeapAdmissionException : IOException("Host storage policy rejected the completed heap dump")
}
