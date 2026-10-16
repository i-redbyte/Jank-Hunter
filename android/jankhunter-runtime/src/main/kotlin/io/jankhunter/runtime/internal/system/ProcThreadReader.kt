package io.jankhunter.runtime.internal.system

import java.io.File
import java.io.IOException

internal data class ProcThreadSnapshot(
    val threads: List<ThreadObservation>,
    val complete: Boolean,
    val failedReads: Int,
    val incompleteReason: String?,
    val javaIdentityCollection: Boolean = false,
    val droppedJavaIdentities: Long = 0,
)

/** Only used on the maintenance lane. Directory enumeration is not an atomic kernel snapshot. */
internal class ProcThreadReader(
    private val enumerate: () -> Iterator<Long>? = {
        File("/proc/self/task").list()?.asSequence()?.mapNotNull { it.toLongOrNull() }?.iterator()
    },
    private val readStat: (Long) -> String? = ::readThreadStat,
    private val limit: Int = 2048,
) {
    init { require(limit in 1..2048) }

    fun read(): ProcThreadSnapshot {
        val ids = try { enumerate() } catch (_: IOException) { null } catch (_: SecurityException) { null }
            ?: return ProcThreadSnapshot(emptyList(), false, 0, "enumeration_unavailable")
        val rows = ArrayList<ThreadObservation>(minOf(limit, 128))
        var failed = 0
        var visited = 0
        while (ids.hasNext() && visited < limit) {
            val tid = ids.next()
            visited++
            val text = try { readStat(tid) } catch (_: IOException) { null } catch (_: SecurityException) { null }
            val row = text?.let(::parseThreadStat)?.takeIf { it.tid == tid }
            if (row == null) failed++ else rows.add(row)
        }
        val reason = when {
            ids.hasNext() -> "thread_limit"
            failed > 0 -> "stat_unavailable"
            else -> null
        }
        return ProcThreadSnapshot(rows, reason == null, failed, reason)
    }
}

private fun readThreadStat(tid: Long): String? = try {
    // A corrupted/unexpected proc source must not cause an unbounded read.
    File("/proc/self/task/$tid/stat").bufferedReader().use { reader ->
        val buffer = CharArray(4097)
        var size = 0
        while (size < buffer.size) {
            val count = reader.read(buffer, size, buffer.size - size)
            if (count < 0) break
            size += count
        }
        if (size > 4096) null else String(buffer, 0, size)
    }
} catch (_: IOException) {
    null
} catch (_: SecurityException) {
    null
}
