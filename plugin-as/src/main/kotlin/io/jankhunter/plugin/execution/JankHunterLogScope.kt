package io.jankhunter.plugin.execution

enum class JankHunterLogScope(
    val label: String,
    val description: String,
) {
    LATEST_LOG(
        "One latest log",
        "Use only the newest .jhlog file from the selected inputs.",
    ),
    LATEST_SESSION_GROUP(
        "Latest session group",
        "Keep the CLI default behavior: latest session group per process, older session files are skipped.",
    ),
    ALL_SELECTED(
        "All selected logs",
        "Aggregate every selected .jhlog. Inspect adds --all-sessions so session files are not dropped.",
    );

    override fun toString(): String = label
}
