package io.jankhunter.sample.graph

import android.graphics.BitmapFactory
import android.os.SystemClock
import io.jankhunter.artti.ArtTiDiagnostics
import java.io.ByteArrayInputStream
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

internal data class JvmtiEvidenceResult(
    val mainThreadWaitMs: Long,
    val allocatedBytes: Long,
    val decodedImageWidth: Int,
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
        val decodedWidth = decodeSyntheticImageOnMainThread()
        ArtTiDiagnostics.captureCurrentThreadStack()

        holder.join(HOLDER_JOIN_TIMEOUT_MS)
        check(!holder.isAlive) { "JVMTI evidence monitor holder did not stop" }
        holderFailure.get()?.let { throw IllegalStateException("JVMTI evidence holder failed", it) }
        return JvmtiEvidenceResult(waitDurationMs, allocatedBytes.get(), decodedWidth)
    }

    private fun decodeSyntheticImageOnMainThread(): Int {
        return BitmapFactory.decodeStream(ByteArrayInputStream(MINIMAL_PNG))?.width ?: 0
    }

    private companion object {
        val MINIMAL_PNG: ByteArray = byteArrayOf(
            0x89.toByte(), 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
            0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
            0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
            0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4.toByte(),
            0x89.toByte(), 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
            0x54, 0x78, 0x9c.toByte(), 0x63, 0x00, 0x01, 0x00,
            0x00, 0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4.toByte(),
            0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
            0xae.toByte(), 0x42, 0x60, 0x82.toByte(),
        )
        const val HOLDER_THREAD_NAME = "JH-JVMTI-MonitorHolder"
        const val HOLDER_START_TIMEOUT_SECONDS = 2L
        const val HOLDER_JOIN_TIMEOUT_MS = 2_000L
        const val MIN_HOLD_DURATION_MS = 100L
        const val MAX_HOLD_DURATION_MS = 2_000L
        const val PRESSURE_ARRAY_COUNT = 16
        const val PRESSURE_ARRAY_BYTES = 256 * 1024
    }
}
