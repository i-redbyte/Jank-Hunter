package io.jankhunter.sample.ui

internal enum class SampleActionTone {
    PRIMARY,
    SUCCESS,
    WARNING,
    DANGER,
    EMPHASIS,
}

internal data class SampleAction(
    val label: String,
    val tone: SampleActionTone = SampleActionTone.PRIMARY,
    val onClick: () -> Unit,
)

internal data class SampleSection(
    val title: String,
    val subtitle: String,
    val detail: String? = null,
    val actions: List<SampleAction>,
)
