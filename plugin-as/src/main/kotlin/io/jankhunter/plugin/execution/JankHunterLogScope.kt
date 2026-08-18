package io.jankhunter.plugin.execution

enum class JankHunterLogScope(
    val label: String,
    val description: String,
) {
    LATEST_LOG(
        "Latest session log",
        "Use every process and segment from the run with the greatest canonical date and numeric index.",
    ),
    ALL_SELECTED(
        "All selected logs (--all-sessions)",
        "Aggregate every selected .jhlog. Inspect adds --all-sessions so session files are not dropped.",
    );

    override fun toString(): String = label
}
