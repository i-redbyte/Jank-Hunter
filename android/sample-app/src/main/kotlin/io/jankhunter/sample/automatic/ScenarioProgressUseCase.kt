package io.jankhunter.sample.automatic

import android.content.res.Resources
import androidx.annotation.StringRes
import io.jankhunter.sample.R

internal enum class ScenarioStep(
    @param:StringRes val labelRes: Int,
    @param:StringRes val titleRes: Int,
    @param:StringRes val descriptionRes: Int,
    val factRes: List<Int>,
    val screenName: String,
    val actions: List<ScenarioAction> = emptyList(),
) {
    LAUNCH(
        labelRes = R.string.auto_stage_launch,
        titleRes = R.string.auto_launch_title,
        descriptionRes = R.string.auto_launch_description,
        factRes = listOf(
            R.string.auto_launch_fact_real,
            R.string.auto_launch_fact_system,
            R.string.auto_launch_fact_report,
        ),
        screenName = "sample.compose.launch",
        actions = listOf(ScenarioAction.OPEN_MANUAL_MODE),
    ),
    BASELINE(
        labelRes = R.string.auto_stage_baseline,
        titleRes = R.string.auto_baseline_title,
        descriptionRes = R.string.auto_baseline_description,
        factRes = listOf(
            R.string.auto_baseline_fact_frames,
            R.string.auto_baseline_fact_object,
            R.string.auto_baseline_fact_executor,
        ),
        screenName = "sample.compose.baseline",
    ),
    UI_CPU(
        labelRes = R.string.auto_stage_ui_cpu,
        titleRes = R.string.auto_ui_cpu_title,
        descriptionRes = R.string.auto_ui_cpu_description,
        factRes = listOf(
            R.string.auto_ui_cpu_fact_stall,
            R.string.auto_ui_cpu_fact_cpu,
            R.string.auto_ui_cpu_fact_queue,
        ),
        screenName = "sample.compose.ui_cpu",
    ),
    NETWORK(
        labelRes = R.string.auto_stage_network,
        titleRes = R.string.auto_network_title,
        descriptionRes = R.string.auto_network_description,
        factRes = listOf(
            R.string.auto_network_fact_http,
            R.string.auto_network_fact_failure,
            R.string.auto_network_fact_websocket,
        ),
        screenName = "sample.compose.network",
    ),
    DATABASE(
        labelRes = R.string.auto_stage_database,
        titleRes = R.string.auto_database_title,
        descriptionRes = R.string.auto_database_description,
        factRes = listOf(
            R.string.auto_database_fact_threads,
            R.string.auto_database_fact_transactions,
            R.string.auto_database_fact_privacy,
        ),
        screenName = "sample.compose.database",
    ),
    MEMORY(
        labelRes = R.string.auto_stage_memory,
        titleRes = R.string.auto_memory_title,
        descriptionRes = R.string.auto_memory_description,
        factRes = listOf(
            R.string.auto_memory_fact_allocations,
            R.string.auto_memory_fact_retention,
            R.string.auto_memory_fact_logs,
        ),
        screenName = "sample.compose.memory",
    ),
    RESULT(
        labelRes = R.string.auto_stage_result,
        titleRes = R.string.auto_result_title,
        descriptionRes = R.string.auto_result_description,
        factRes = listOf(
            R.string.auto_result_fact_truth,
            R.string.auto_result_fact_heap,
            R.string.auto_result_fact_cli,
        ),
        screenName = "sample.compose.result",
        actions = listOf(
            ScenarioAction.SHARE_DIAGNOSTICS,
            ScenarioAction.OPEN_MANUAL_MODE,
        ),
    ),
}

internal class ScenarioProgressUseCase(private val resources: Resources) {
    val total: Int = ScenarioStep.entries.lastIndex

    operator fun invoke(step: ScenarioStep): String {
        return resources.getString(
            R.string.auto_progress_format,
            step.ordinal,
            total,
            resources.getString(step.labelRes),
        )
    }
}
