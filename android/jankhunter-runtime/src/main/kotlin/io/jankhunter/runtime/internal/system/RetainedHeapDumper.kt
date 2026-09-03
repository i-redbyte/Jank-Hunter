package io.jankhunter.runtime.internal.system

import android.os.Debug
import io.jankhunter.runtime.JankHunterBinaryStorage
import io.jankhunter.runtime.RuntimeLongSource
import java.io.File
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
) {
    @Volatile
    private var binaryStorage = binaryStorage
    private val maxCount = maxDumpCount.coerceAtLeast(0)
    private val minAgeMs = minRetainedAgeMs.coerceAtLeast(0L)
    private val lastDumpAtMs = AtomicLong(Long.MIN_VALUE)
    private val dumpCount = AtomicInteger()

    fun maybeDump(className: String?, holder: String?, ageMs: Long, count: Long): Result {
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
            val file = dumpToFile(fileName, storage)
            if (!enforceRetention(storage, file)) {
                deleteDump(storage, file)
                throw RetentionCleanupException()
            }
            Result.Dumped(file, safeName(className), safeName(holder), ageMs, count)
        } catch (error: Throwable) {
            dumpCount.decrementAndGet()
            lastDumpAtMs.compareAndSet(now, last)
            Result.Failed(error.javaClass.simpleName ?: "error")
        }
    }

    private fun dumpToFile(fileName: String, storage: JankHunterBinaryStorage?): File {
        if (storage == null) {
            directory.mkdirs()
            val file = File(directory, fileName)
            return try {
                dumpHprof(file.absolutePath)
                file
            } catch (error: Throwable) {
                file.delete()
                throw error
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
                runCatching { artifact.abort() }
            }
        }
    }

    private fun enforceRetention(storage: JankHunterBinaryStorage?, newest: File): Boolean {
        val paths = try {
            if (storage == null) {
                directory.listFiles { file -> file.isFile }.orEmpty().map(File::getAbsolutePath)
            } else {
                storage.listFiles()
            }
        } catch (_: Throwable) {
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
                !file.exists() || file.delete()
            } else {
                storage.delete(file.name)
                storage.listFiles().none { path -> File(path).name == file.name }
            }
        } catch (_: Throwable) {
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
}
