package io.jankhunter.sample.graph

import io.jankhunter.runtime.JankHunterTelemetry

import android.os.SystemClock

internal class PerformanceScenarioUseCase(
    private val calculator: CheckoutCalculator,
    private val renderer: CheckoutRenderer,
) {
    fun calculateFor(durationMs: Long): Long {
        var checksum = 0L
        JankHunterTelemetry.withOwner(CheckoutCalculator::class.java.name) {
            checksum = calculator.calculateFor(durationMs)
        }
        return checksum
    }

    fun renderFor(durationMs: Long) {
        JankHunterTelemetry.withOwner(CheckoutRenderer::class.java.name) {
            renderer.renderFor(durationMs)
        }
    }
}

internal class CheckoutCalculator {
    fun calculateFor(durationMs: Long): Long {
        val startedAt = SystemClock.elapsedRealtime()
        var checksum = 17L
        while (SystemClock.elapsedRealtime() - startedAt < durationMs) {
            checksum = checksum * 31L + System.nanoTime()
        }
        return checksum and Long.MAX_VALUE
    }
}

internal class CheckoutRenderer {
    fun renderFor(durationMs: Long) {
        SystemClock.sleep(durationMs)
    }
}
