package io.jankhunter.plugin.execution

enum class JankHunterMode(
    val label: String,
    val command: String,
    val defaultExtension: String?,
    val hint: String,
) {
    INSPECT("Inspect", "inspect", "html", "Разобрать один или несколько .jhlog и собрать HTML-отчет."),
    COMPARE("Compare", "compare", "html", "Сравнить базовый и кандидатный прогоны, затем собрать HTML-отчет."),
    PROBLEMS("Problems", "problems", "csv", "Выгрузить проблемные места в CSV или JSON для ревью и triage."),
    SCORECARD("Scorecard", "scorecard", "json", "Собрать JSON scorecard для проверки кандидата относительно базы."),
    SAMPLE("Sample", "sample", "jhlog", "Создать пример .jhlog для быстрой проверки CLI и плагина."),
    VERSION("Version", "version", null, "Показать версию Jank Hunter CLI и формат .jhlog.");

    override fun toString(): String = label
}
