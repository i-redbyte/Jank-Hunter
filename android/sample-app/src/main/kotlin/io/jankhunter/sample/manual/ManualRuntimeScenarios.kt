package io.jankhunter.sample.manual

import io.jankhunter.runtime.JankHunterTelemetry

import io.jankhunter.sample.R
import io.jankhunter.runtime.JankHunter

internal class ManualRuntimeScenarios(
    private val text: SampleText,
    private val stateSink: ManualStateSink,
) {
    val handlers: Map<ManualAction, ManualActionHandler> = mapOf(
        ManualAction.ENABLE_RUNTIME to { enable() },
        ManualAction.DISABLE_RUNTIME to { disable() },
        ManualAction.RECORD_FLAG_PROBE to { recordProbe() },
    )

    fun enable() {
        val enabled = JankHunter.setRuntimeEnabled(true, "sample_manual_feature_flag")
        refresh(
            if (enabled) text(R.string.status_runtime_enabled)
            else text(R.string.status_runtime_enable_failed),
        )
    }

    fun disable() {
        JankHunter.setRuntimeEnabled(false, "sample_manual_feature_flag")
        refresh(text(R.string.status_runtime_disabled))
    }

    fun recordProbe() {
        JankHunterTelemetry.counter("sample.feature_flag.probe.count", 1)
        JankHunterTelemetry.gauge(
            "sample.feature_flag.runtime_enabled",
            if (JankHunter.isRuntimeEnabled()) 1 else 0,
        )
        refresh(text(R.string.status_runtime_probe_recorded))
    }

    fun refresh(reason: String) {
        val flag = if (JankHunter.isRuntimeEnabled()) {
            text(R.string.runtime_flag_on)
        } else {
            text(R.string.runtime_flag_off)
        }
        val collection = if (JankHunter.isStarted()) {
            text(R.string.runtime_collecting)
        } else {
            text(R.string.runtime_paused)
        }
        stateSink.emit(
            ManualStateUpdate.Runtime(
                value = text(R.string.runtime_flag_line, flag, collection, reason),
                reason = reason,
            ),
        )
    }
}
