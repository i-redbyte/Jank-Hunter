package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import java.util.concurrent.atomic.AtomicInteger

/** The hook and graph owners must both finish before their shared writer can be sealed. */
internal class RuntimeWriterDrain(private val writer: AsyncLogWriter, private val closeTimeoutMs: Long) {
    private val remaining = AtomicInteger(2)

    fun complete() {
        if (remaining.decrementAndGet() == 0) {
            RuntimeHookGuard.swallow { writer.close(closeTimeoutMs.coerceAtLeast(1L)) }
        }
    }
}
