package io.jankhunter.runtime

import android.os.SystemClock
import java.util.concurrent.AbstractExecutorService
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class JankHunterExecutor internal constructor(
    private val delegate: Executor,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : Executor {
    private val queued = AtomicInteger()

    override fun execute(command: Runnable) {
        val enqueuedAtMs = clock()
        queued.incrementAndGet()
        JankHunter.recordExecutorSnapshot(name, delegate, queued.get())
        delegate.execute {
            queued.decrementAndGet()
            JankHunter.recordExecutorWait(name, ownerName, clock() - enqueuedAtMs)
            JankHunter.recordExecutorSnapshot(name, delegate, queued.get())
            JankHunter.runExecutorTask(name, ownerName, command, clock)
        }
    }
}

class JankHunterExecutorService internal constructor(
    private val delegate: ExecutorService,
    private val name: String?,
    private val ownerName: String?,
    private val clock: () -> Long = SystemClock::elapsedRealtime,
) : AbstractExecutorService() {
    private val queued = AtomicInteger()

    override fun execute(command: Runnable) {
        val enqueuedAtMs = clock()
        queued.incrementAndGet()
        JankHunter.recordExecutorSnapshot(name, delegate, queued.get())
        delegate.execute {
            queued.decrementAndGet()
            JankHunter.recordExecutorWait(name, ownerName, clock() - enqueuedAtMs)
            JankHunter.recordExecutorSnapshot(name, delegate, queued.get())
            JankHunter.runExecutorTask(name, ownerName, command, clock)
        }
    }

    override fun shutdown() {
        delegate.shutdown()
    }

    override fun shutdownNow(): MutableList<Runnable> = delegate.shutdownNow()

    override fun isShutdown(): Boolean = delegate.isShutdown

    override fun isTerminated(): Boolean = delegate.isTerminated

    override fun awaitTermination(timeout: Long, unit: TimeUnit): Boolean {
        return delegate.awaitTermination(timeout, unit)
    }

}

internal fun metricExecutorName(name: String?): String {
    return name
        ?.takeIf { it.isNotBlank() }
        ?.replace(Regex("[^A-Za-z0-9_.-]+"), "_")
        ?: "unknown"
}

internal fun ThreadPoolExecutor.snapshotActiveCount(): Int = activeCount
