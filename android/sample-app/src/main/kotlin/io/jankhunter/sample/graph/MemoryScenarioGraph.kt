package io.jankhunter.sample.graph

import io.jankhunter.runtime.JankHunterTelemetry

import android.util.Log
import io.jankhunter.sample.ReleasedCheckoutProbe
import io.jankhunter.sample.RetainedCheckoutCache
import io.jankhunter.sample.RetainedCheckoutScreen
import io.jankhunter.sample.SampleApplication

internal class MemoryScenarioUseCase(
    private val allocator: CheckoutMemoryAllocator,
    private val retentionRepository: CheckoutRetentionRepository,
    private val analytics: CheckoutAnalytics,
) {
    fun allocateReleased(): Long {
        return allocator.allocateReleased()
    }

    fun retainPressure(): Long {
        return allocator.retainPressure()
    }

    fun watchReleased() {
        retentionRepository.watchReleased()
    }

    fun retainScreen(activityReference: Any) {
        retentionRepository.retainScreen(activityReference)
    }

    fun retainCache() {
        retentionRepository.retainCache()
    }

    fun recordQuietLogs() {
        analytics.recordQuietLogs()
    }

    fun recordBurstLogs() {
        analytics.recordBurstLogs()
    }
}

internal class CheckoutMemoryAllocator(
    private val application: SampleApplication,
) {
    fun allocateReleased(): Long {
        var checksum = 0L
        repeat(RELEASED_ALLOCATION_COUNT) { index ->
            val allocation = ByteArray(RELEASED_ALLOCATION_BYTES) { (it + index).toByte() }
            checksum += allocation[index].toLong()
        }
        return checksum
    }

    fun retainPressure(): Long {
        repeat(PRESSURE_ALLOCATION_COUNT) {
            application.memoryPressure += ByteArray(PRESSURE_ALLOCATION_BYTES)
        }
        return application.memoryPressure.sumOf { it.size.toLong() } / 1024L
    }

    private companion object {
        const val RELEASED_ALLOCATION_COUNT = 12
        const val RELEASED_ALLOCATION_BYTES = 256 * 1024
        const val PRESSURE_ALLOCATION_COUNT = 4
        const val PRESSURE_ALLOCATION_BYTES = 2 * 1024 * 1024
    }
}

internal class CheckoutRetentionRepository(
    private val application: SampleApplication,
) {
    fun watchReleased() {
        val released = ReleasedCheckoutProbe()
        JankHunterTelemetry.watch(
            released,
            ReleasedCheckoutProbe::class.java.name,
            "sample.auto.retention.released",
        )
    }

    fun retainScreen(activityReference: Any) {
        val retained = RetainedCheckoutScreen(activityReference, ByteArray(RETAINED_SCREEN_BYTES))
        application.retainedObjects += retained
        JankHunterTelemetry.watch(
            retained,
            RetainedCheckoutScreen::class.java.name,
            "sample.auto.retention.activity_registry",
        )
    }

    fun retainCache() {
        repeat(RETAINED_CACHE_COUNT) { index ->
            val retained = RetainedCheckoutCache(index, ByteArray(RETAINED_CACHE_BYTES))
            application.retainedObjects += retained
            JankHunterTelemetry.watch(
                retained,
                RetainedCheckoutCache::class.java.name,
                "sample.auto.retention.checkout_cache",
            )
        }
    }

    private companion object {
        const val RETAINED_SCREEN_BYTES = 512 * 1024
        const val RETAINED_CACHE_COUNT = 3
        const val RETAINED_CACHE_BYTES = 384 * 1024
    }
}

internal class CheckoutAnalytics {
    fun recordQuietLogs() {
        repeat(QUIET_LOG_COUNT) { index ->
            Log.i(TAG, "quiet#$index")
        }
    }

    fun recordBurstLogs() {
        repeat(NOISY_LOG_COUNT) {
            Log.e(TAG, "checkout")
        }
    }

    private companion object {
        const val TAG = "CheckoutAnalytics"
        const val QUIET_LOG_COUNT = 3
        const val NOISY_LOG_COUNT = 60
    }
}
