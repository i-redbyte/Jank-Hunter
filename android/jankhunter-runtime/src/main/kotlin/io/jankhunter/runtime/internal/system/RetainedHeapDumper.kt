package io.jankhunter.runtime.internal.system

import android.os.Debug
import io.jankhunter.runtime.JankHunterBinaryStorage
import java.io.File
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong

internal class RetainedHeapDumper(
    private val directory: File,
    private val binaryStorage: JankHunterBinaryStorage? = null,
    private val minIntervalMs: Long,
    maxDumpCount: Int,
    minRetainedAgeMs: Long = 0L,
    private val clock: () -> Long = { android.os.SystemClock.elapsedRealtime() },
    private val wallClock: () -> Long = { System.currentTimeMillis() },
    private val dumpHprof: (String) -> Unit = { path -> Debug.dumpHprofData(path) },
) {
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
        val now = clock()
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
            val fileName = "retained-${wallClock()}-${safeName(className)}-${nextCount}.hprof"
            val file = dumpToFile(fileName)
            Result.Dumped(file, safeName(className), safeName(holder), ageMs, count)
        } catch (error: Throwable) {
            dumpCount.decrementAndGet()
            lastDumpAtMs.compareAndSet(now, last)
            Result.Failed(error.javaClass.simpleName ?: "error")
        }
    }

    private fun dumpToFile(fileName: String): File {
        val storage = binaryStorage
        if (storage == null) {
            directory.mkdirs()
            val file = File(directory, fileName)
            dumpHprof(file.absolutePath)
            return file
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
    }
}
