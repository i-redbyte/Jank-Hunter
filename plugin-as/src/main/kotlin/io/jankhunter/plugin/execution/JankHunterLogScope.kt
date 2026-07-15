package io.jankhunter.plugin.execution

enum class JankHunterLogScope(
    val label: String,
    val description: String,
) {
    LATEST_LOG(
        "Latest session log",
        "Use the greatest date and numeric index from canonical jh-session-log.YYYY-MM-DD.<index>.jhlog names.",
    ),
    ALL_SELECTED(
        "All selected logs (--all-sessions)",
        "Aggregate every selected .jhlog. Inspect adds --all-sessions so session files are not dropped.",
    );

    override fun toString(): String = label
}
