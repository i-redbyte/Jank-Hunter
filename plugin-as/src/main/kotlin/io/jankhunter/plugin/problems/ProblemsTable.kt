package io.jankhunter.plugin.problems

data class ProblemsTable(
    val columns: List<String>,
    val rows: List<Map<String, String>>,
) {
    val isEmpty: Boolean get() = columns.isEmpty() || rows.isEmpty()
}
