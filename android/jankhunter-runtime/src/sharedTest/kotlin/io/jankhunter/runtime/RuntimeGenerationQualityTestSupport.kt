package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.LogQualityCounters

internal fun generationQuality(writer: AsyncLogWriter, id: Int): Long {
    val field = AsyncLogWriter::class.java.getDeclaredField("quality").apply { isAccessible = true }
    val counters = field.get(writer) as LogQualityCounters
    return counters.snapshot().firstOrNull { it.counterId == id }?.value ?: 0L
}
