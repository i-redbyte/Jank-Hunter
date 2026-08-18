package io.jankhunter.runtime

import io.jankhunter.runtime.internal.saturatingAdd
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReferenceArray

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

internal inline fun <T> registerFirstAvailable(
    registry: AtomicReferenceArray<T>,
    create: () -> T,
): T? {
    var firstAvailable = -1
    for (index in 0 until registry.length()) {
        if (registry.get(index) == null) {
            firstAvailable = index
            break
        }
    }
    if (firstAvailable < 0) return null
    val value = create()
    for (offset in 0 until registry.length()) {
        val index = (firstAvailable + offset) % registry.length()
        if (registry.compareAndSet(index, null, value)) return value
    }
    return null
}
