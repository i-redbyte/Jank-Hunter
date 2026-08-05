package io.jankhunter.sample.manual

import android.content.Context
import io.jankhunter.sample.R
import io.jankhunter.sample.ui.SampleAction
import io.jankhunter.sample.ui.SampleActionTone
import io.jankhunter.sample.ui.SampleSection

internal object ManualControlsUiFactory {
    private val sections = listOf(
        SectionSpec(
            title = R.string.section_control_tower,
            subtitle = R.string.section_control_tower_subtitle,
            actions = listOf(
                ActionSpec(R.string.action_run_clean_baseline, ManualAction.RUN_CLEAN_BASELINE, SampleActionTone.SUCCESS),
                ActionSpec(R.string.action_run_noisy_candidate, ManualAction.RUN_NOISY_CANDIDATE, SampleActionTone.EMPHASIS),
                ActionSpec(R.string.action_flush_diagnostics, ManualAction.FLUSH_DIAGNOSTICS),
                ActionSpec(R.string.action_share_diagnostics, ManualAction.SHARE_DIAGNOSTICS),
            ),
        ),
        SectionSpec(
            title = R.string.section_feature_flag,
            subtitle = R.string.section_feature_flag_subtitle,
            detail = DetailSource.RUNTIME,
            actions = listOf(
                ActionSpec(R.string.action_enable_sdk_runtime, ManualAction.ENABLE_RUNTIME, SampleActionTone.SUCCESS),
                ActionSpec(R.string.action_disable_sdk_runtime, ManualAction.DISABLE_RUNTIME, SampleActionTone.WARNING),
                ActionSpec(R.string.action_record_flag_probe, ManualAction.RECORD_FLAG_PROBE),
            ),
        ),
        SectionSpec(
            title = R.string.section_performance_lab,
            subtitle = R.string.section_performance_lab_subtitle,
            actions = listOf(
                ActionSpec(R.string.action_ui_stall, ManualAction.UI_STALL, SampleActionTone.WARNING),
                ActionSpec(R.string.action_background_work, ManualAction.BACKGROUND_WORK),
                ActionSpec(R.string.action_http_success, ManualAction.HTTP_SUCCESS, SampleActionTone.SUCCESS),
                ActionSpec(R.string.action_http_503, ManualAction.HTTP_503, SampleActionTone.DANGER),
                ActionSpec(R.string.action_memory_pressure, ManualAction.MEMORY_PRESSURE, SampleActionTone.EMPHASIS),
                ActionSpec(R.string.action_log_spam, ManualAction.LOG_SPAM, SampleActionTone.DANGER),
                ActionSpec(R.string.action_custom_metrics, ManualAction.CUSTOM_METRICS),
            ),
        ),
        SectionSpec(
            title = R.string.section_leak_lab,
            subtitle = R.string.section_leak_lab_subtitle,
            actions = listOf(
                ActionSpec(R.string.action_clean_object, ManualAction.CLEAN_OBJECT, SampleActionTone.SUCCESS),
                ActionSpec(R.string.action_activity_reference, ManualAction.ACTIVITY_REFERENCE, SampleActionTone.EMPHASIS),
                ActionSpec(R.string.action_view_binding, ManualAction.VIEW_BINDING, SampleActionTone.EMPHASIS),
                ActionSpec(R.string.action_listener_callback, ManualAction.LISTENER_CALLBACK, SampleActionTone.WARNING),
                ActionSpec(R.string.action_cache_entries, ManualAction.CACHE_ENTRIES, SampleActionTone.WARNING),
                ActionSpec(R.string.action_clear_retained_list, ManualAction.CLEAR_RETAINED, SampleActionTone.SUCCESS),
            ),
        ),
        SectionSpec(
            title = R.string.section_leakcanary_benchmark,
            subtitle = R.string.section_leakcanary_benchmark_subtitle,
            detail = DetailSource.LEAK_CANARY,
            actions = listOf(
                ActionSpec(R.string.action_both_clean_object, ManualAction.BOTH_CLEAN_OBJECT, SampleActionTone.SUCCESS),
                ActionSpec(R.string.action_both_retained_object, ManualAction.BOTH_RETAINED_OBJECT, SampleActionTone.EMPHASIS),
                ActionSpec(R.string.action_both_cache_burst, ManualAction.BOTH_CACHE_BURST, SampleActionTone.DANGER),
                ActionSpec(R.string.action_how_to_compare, ManualAction.HOW_TO_COMPARE),
            ),
        ),
        SectionSpec(
            title = R.string.section_compare_lab,
            subtitle = R.string.section_compare_lab_subtitle,
            actions = listOf(
                ActionSpec(R.string.action_candidate_leak_burst, ManualAction.CANDIDATE_LEAK_BURST, SampleActionTone.DANGER),
                ActionSpec(R.string.action_candidate_perf_burst, ManualAction.CANDIDATE_PERF_BURST, SampleActionTone.DANGER),
                ActionSpec(R.string.action_pull_report_hint, ManualAction.PULL_REPORT_HINT),
            ),
        ),
    )

    fun create(
        context: Context,
        state: ManualControlsUiState,
        onAction: (ManualAction) -> Unit,
    ): List<SampleSection> {
        return sections.map { section ->
            SampleSection(
                title = context.getString(section.title),
                subtitle = context.getString(section.subtitle),
                detail = section.detail.resolve(state),
                actions = section.actions.map { action ->
                    SampleAction(
                        label = context.getString(action.label),
                        tone = action.tone,
                        onClick = { onAction(action.action) },
                    )
                },
            )
        }
    }

    private fun DetailSource.resolve(state: ManualControlsUiState): String? {
        return when (this) {
            DetailSource.NONE -> null
            DetailSource.RUNTIME -> state.runtime
            DetailSource.LEAK_CANARY -> state.leakCanary
        }
    }

    private data class SectionSpec(
        val title: Int,
        val subtitle: Int,
        val detail: DetailSource = DetailSource.NONE,
        val actions: List<ActionSpec>,
    )

    private data class ActionSpec(
        val label: Int,
        val action: ManualAction,
        val tone: SampleActionTone = SampleActionTone.PRIMARY,
    )

    private enum class DetailSource {
        NONE,
        RUNTIME,
        LEAK_CANARY,
    }
}
