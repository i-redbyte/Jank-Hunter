package io.jankhunter.sample.graph

import android.os.SystemClock
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

internal data class JvmtiEvidenceResult(
    val mainThreadWaitMs: Long,
    val allocatedBytes: Long,
)

/** Creates real ART events: monitor contention on the caller plus allocation pressure and GC. */
internal class JvmtiEvidenceScenario {
    private val monitor = Any()

    fun blockMainThread(holdDurationMs: Long): JvmtiEvidenceResult {
        require(holdDurationMs in MIN_HOLD_DURATION_MS..MAX_HOLD_DURATION_MS)
        val holderReady = CountDownLatch(1)
        val allocatedBytes = AtomicLong()
        val holderFailure = AtomicReference<Throwable?>()
        val holder = Thread(
            {
                runCatching {
                    synchronized(monitor) {
                        val startedAt = SystemClock.elapsedRealtime()
                        holderReady.countDown()
                        val pressure = Array(PRESSURE_ARRAY_COUNT) { ByteArray(PRESSURE_ARRAY_BYTES) }
                        allocatedBytes.set(pressure.sumOf { it.size.toLong() })
                        Runtime.getRuntime().gc()
                        val remainingMs = holdDurationMs - (SystemClock.elapsedRealtime() - startedAt)
                        if (remainingMs > 0L) SystemClock.sleep(remainingMs)
                        check(pressure.sumOf { it.size.toLong() } == allocatedBytes.get())
                    }
                }.onFailure(holderFailure::set)
                holderReady.countDown()
            },
            HOLDER_THREAD_NAME,
        ).apply { start() }

        check(holderReady.await(HOLDER_START_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
            "JVMTI evidence monitor holder did not start"
        }
        holderFailure.get()?.let { throw IllegalStateException("JVMTI evidence holder failed", it) }

        val waitStartedAt = SystemClock.elapsedRealtime()
        synchronized(monitor) { Unit }
        val waitDurationMs = SystemClock.elapsedRealtime() - waitStartedAt

        holder.join(HOLDER_JOIN_TIMEOUT_MS)
        check(!holder.isAlive) { "JVMTI evidence monitor holder did not stop" }
        holderFailure.get()?.let { throw IllegalStateException("JVMTI evidence holder failed", it) }
        return JvmtiEvidenceResult(waitDurationMs, allocatedBytes.get())
    }

    private companion object {
        const val HOLDER_THREAD_NAME = "JH-JVMTI-MonitorHolder"
        const val HOLDER_START_TIMEOUT_SECONDS = 2L
        const val HOLDER_JOIN_TIMEOUT_MS = 2_000L
        const val MIN_HOLD_DURATION_MS = 100L
        const val MAX_HOLD_DURATION_MS = 2_000L
        const val PRESSURE_ARRAY_COUNT = 16
        const val PRESSURE_ARRAY_BYTES = 256 * 1024
    }
}
