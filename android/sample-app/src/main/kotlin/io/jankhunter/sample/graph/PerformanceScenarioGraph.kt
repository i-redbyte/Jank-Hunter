package io.jankhunter.sample.graph

import android.os.SystemClock
import io.jankhunter.runtime.JankHunter

internal class PerformanceScenarioUseCase(
    private val calculator: CheckoutCalculator,
    private val renderer: CheckoutRenderer,
    private val jvmtiEvidence: JvmtiEvidenceScenario,
) {
    fun calculateFor(durationMs: Long): Long {
        var checksum = 0L
        JankHunter.withOwner(CheckoutCalculator::class.java.name) {
            checksum = calculator.calculateFor(durationMs)
        }
        return checksum
    }

    fun renderFor(durationMs: Long) {
        JankHunter.withOwner(CheckoutRenderer::class.java.name) {
            renderer.renderFor(durationMs)
        }
    }

    fun collectJvmtiEvidence(holdDurationMs: Long): JvmtiEvidenceResult {
        var result: JvmtiEvidenceResult? = null
        JankHunter.withOwner(JvmtiEvidenceScenario::class.java.name) {
            result = jvmtiEvidence.blockMainThread(holdDurationMs)
        }
        return checkNotNull(result)
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
