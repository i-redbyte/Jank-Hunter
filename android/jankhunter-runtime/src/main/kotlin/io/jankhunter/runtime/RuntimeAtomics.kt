package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.util.concurrent.atomic.AtomicLong

internal fun advanceRuntimeEpoch(epoch: AtomicLong) {
    while (true) {
        val current = epoch.get()
        val next = if (current == Long.MAX_VALUE) 1L else current + 1L
        if (epoch.compareAndSet(current, next)) return
    }
}

internal fun recordAndResetQuality(writer: AsyncLogWriter, counterId: Int, counter: AtomicLong) {
    val value = counter.getAndSet(0L)
    if (value > 0L) writer.recordQuality(counterId, value)
}

internal fun addSaturating(target: AtomicLong, delta: Long) {
    var current = target.get()
    while (!target.compareAndSet(current, saturatingAdd(current, delta))) {
        current = target.get()
    }
}
