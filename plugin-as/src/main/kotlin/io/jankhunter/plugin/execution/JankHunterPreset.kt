package io.jankhunter.plugin.execution

enum class JankHunterPreset(
    val label: String,
    val description: String,
) {
    CUSTOM("Custom", "Не менять поля автоматически."),
    FAST_INSPECT("Быстрый inspect", "Inspect без heap и без JSON, с HTML-отчетом внутри IDE."),
    INSPECT_WITH_HEAP("Inspect с heap", "Inspect с HTML-отчетом и heap-полями, если они уже заполнены."),
    COMPARE_WITH_HEAP("Compare с heap", "Compare с presentation mode и полями heap для базы и кандидата."),
    PROBLEMS_CSV("Problems CSV", "Выгрузка code-problems в CSV и открытие таблицы в IDE."),
    CI_SCORECARD("CI scorecard", "JSON scorecard без автоматического открытия отчета.");

    override fun toString(): String = label
}
