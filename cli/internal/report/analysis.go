package report

import (
	"fmt"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type ReportAnalysis struct {
	Severity        string
	Status          string
	Summary         string
	Findings        []ReportFinding
	Recommendations []string
}

type ReportFinding struct {
	Severity string
	Title    string
	Detail   string
}

func inspectAnalysis(summary analyze.Summary, lang string) ReportAnalysis {
	builder := analysisBuilder{lang: lang, severity: "ok"}
	builder.add("ok", text(lang, "Coverage", "Покрытие"), textf(lang,
		"Analyzed %d events from %d log(s).",
		"Проанализировано событий: %d, логов: %d.",
		summary.EventCount,
		summary.LogCount,
	))

	if summary.EventCount < 50 {
		builder.add("medium", text(lang, "Low sample size", "Малая выборка"), text(lang,
			"The run is small, so the verdict is useful as smoke-test feedback but not as a release-quality performance conclusion.",
			"Прогон небольшой, поэтому вердикт полезен как быстрый дымовой тест, но не как финальное перфоманс-заключение для релиза.",
		))
		builder.recommend(text(lang,
			"Collect several runs per scenario before trusting small deltas.",
			"Соберите несколько прогонов на сценарий, прежде чем доверять небольшим изменениям.",
		))
	}

	httpFailureRate := percentInt(summary.HTTPFailed, summary.HTTPCount)
	switch {
	case summary.HTTPCount > 0 && httpFailureRate >= 10:
		builder.add("high", text(lang, "HTTP failures are elevated", "Повышенный уровень HTTP-ошибок"), textf(lang,
			"%d of %d HTTP calls failed or returned 5xx (%.1f%%).",
			"%d из %d HTTP-вызовов завершились ошибкой или 5xx (%.1f%%).",
			summary.HTTPFailed,
			summary.HTTPCount,
			httpFailureRate,
		))
		builder.recommend(text(lang,
			"Inspect failing routes first: transport failures and 5xx responses often dominate user-visible performance.",
			"Сначала проверьте проблемные маршруты: транспортные ошибки и ответы 5xx часто сильнее всего портят пользовательский сценарий.",
		))
	case summary.HTTPCount > 0 && summary.HTTPFailed > 0:
		builder.add("medium", text(lang, "HTTP failures detected", "Обнаружены HTTP-ошибки"), textf(lang,
			"%d of %d HTTP calls failed or returned 5xx (%.1f%%).",
			"%d из %d HTTP-вызовов завершились ошибкой или 5xx (%.1f%%).",
			summary.HTTPFailed,
			summary.HTTPCount,
			httpFailureRate,
		))
	}

	switch {
	case summary.HTTPP95MS >= 1500:
		builder.add("high", text(lang, "HTTP p95 is very slow", "HTTP p95 очень высокий"), textf(lang,
			"HTTP p95 is %d ms; this is likely user-visible on interactive flows.",
			"HTTP p95 = %d мс; это почти наверняка заметно пользователю в интерактивных сценариях.",
			summary.HTTPP95MS,
		))
		builder.recommend(text(lang,
			"Sort routes by p95 and TTFB; separate backend latency from DNS/connect/TLS overhead.",
			"Отсортируйте маршруты по p95 и TTFB; отделите задержку сервера от накладных расходов DNS, соединения и TLS.",
		))
	case summary.HTTPP95MS >= 700:
		builder.add("medium", text(lang, "HTTP p95 needs attention", "HTTP p95 требует внимания"), textf(lang,
			"HTTP p95 is %d ms.",
			"HTTP p95 = %d мс.",
			summary.HTTPP95MS,
		))
	}

	switch {
	case summary.UIJankPct >= 10:
		builder.add("high", text(lang, "UI jank is high", "Высокая доля подтормаживаний UI"), textf(lang,
			"Janky frames are %.2f%% of all observed frames.",
			"Медленные UI-кадры составляют %.2f%% всех наблюдаемых кадров.",
			summary.UIJankPct,
		))
		builder.recommend(text(lang,
			"Open the UI and owner sections together: main-thread stalls often explain the worst screen jank.",
			"Смотрите разделы UI и источников вместе: паузы главного потока часто объясняют худшие подтормаживания экранов.",
		))
	case summary.UIJankPct >= 3:
		builder.add("medium", text(lang, "UI jank is noticeable", "Подтормаживания UI заметны"), textf(lang,
			"Janky frames are %.2f%% of all observed frames.",
			"Медленные UI-кадры составляют %.2f%% всех наблюдаемых кадров.",
			summary.UIJankPct,
		))
	}

	switch {
	case summary.UIAvgFPS > 0 && summary.UIAvgFPS < 45:
		builder.add("high", text(lang, "Average FPS is low", "Низкий средний FPS"), textf(lang,
			"Average FPS is %.1f.",
			"Средний FPS = %.1f.",
			summary.UIAvgFPS,
		))
	case summary.UIAvgFPS > 0 && summary.UIAvgFPS < 55:
		builder.add("medium", text(lang, "Average FPS is below target", "Средний FPS ниже целевого"), textf(lang,
			"Average FPS is %.1f.",
			"Средний FPS = %.1f.",
			summary.UIAvgFPS,
		))
	}

	switch {
	case summary.StallMaxMS >= 1000:
		builder.add("high", text(lang, "Long main-thread stall", "Длинная пауза главного потока"), textf(lang,
			"Max observed stall is %d ms.",
			"Максимальная пауза главного потока = %d мс.",
			summary.StallMaxMS,
		))
		builder.recommend(text(lang,
			"Treat stalls above one second as release blockers for the affected flow.",
			"Паузы больше одной секунды стоит считать релизным блокером для затронутого сценария.",
		))
	case summary.StallMaxMS >= 250:
		builder.add("medium", text(lang, "Main-thread stall detected", "Обнаружена пауза главного потока"), textf(lang,
			"Max observed stall is %d ms.",
			"Максимальная пауза главного потока = %d мс.",
			summary.StallMaxMS,
		))
	}

	if summary.LowMemoryCount > 0 {
		builder.add("high", text(lang, "Low-memory samples present", "Есть сигналы низкой памяти"), textf(lang,
			"%d context samples reported low-memory state.",
			"%d снимков контекста сообщили состояние низкой памяти.",
			summary.LowMemoryCount,
		))
		builder.recommend(text(lang,
			"Correlate low-memory samples with PSS growth and retained objects.",
			"Сопоставьте сигналы низкой памяти с ростом PSS и удержанными объектами.",
		))
	}
	if summary.Retained > 0 {
		severity := "medium"
		if summary.Retained >= 10 {
			severity = "high"
		}
		builder.add(severity, text(lang, "Retained objects detected", "Обнаружены удержанные объекты"), textf(lang,
			"Retained object count is %d.",
			"Количество удержанных объектов: %d.",
			summary.Retained,
		))
		builder.recommend(text(lang,
			"Inspect retained classes and age buckets; old retained objects deserve priority.",
			"Проверьте удержанные классы и возрастные группы; старые удержанные объекты приоритетнее.",
		))
	}

	if len(builder.findingsWithoutCoverage()) == 0 {
		builder.add("ok", text(lang, "No serious issues detected", "Серьезных проблем не найдено"), text(lang,
			"Heuristic thresholds did not find critical regressions or obvious runtime health problems.",
			"Эвристические пороги не нашли критичных регрессий или явных проблем состояния выполнения приложения.",
		))
		builder.recommend(text(lang,
			"Use this report as a baseline and compare future runs against it.",
			"Используйте этот отчет как базу и сравнивайте с ним будущие прогоны.",
		))
	}

	return builder.finish()
}

func compareAnalysis(comparison analyze.Comparison, lang string) ReportAnalysis {
	builder := analysisBuilder{lang: lang, severity: "ok"}
	high := 0
	medium := 0
	for _, delta := range comparison.Deltas {
		switch delta.Severity {
		case "high":
			high++
			builder.add("high", textf(lang, "High regression: %s", "Высокая регрессия: %s", delta.Name), textf(lang,
				"%s -> %s (%s), confidence=%s, sample=%d.",
				"%s -> %s (%s), доверие=%s, выборка=%d.",
				delta.Baseline,
				delta.Candidate,
				delta.Change,
				delta.Confidence,
				delta.SampleSize,
			))
		case "medium":
			medium++
			builder.add("medium", textf(lang, "Medium regression: %s", "Средняя регрессия: %s", delta.Name), textf(lang,
				"%s -> %s (%s), confidence=%s, sample=%d.",
				"%s -> %s (%s), доверие=%s, выборка=%d.",
				delta.Baseline,
				delta.Candidate,
				delta.Change,
				delta.Confidence,
				delta.SampleSize,
			))
		}
	}

	for _, warning := range comparison.Warnings {
		builder.add("medium", text(lang, "Cohort mismatch", "Несовпадение когорт"), warning)
	}

	switch {
	case high > 0:
		builder.recommend(text(lang,
			"Do not merge/release before investigating high-severity deltas.",
			"Не выполняйте слияние или релиз до разбора изменений высокой серьезности.",
		))
	case medium > 0:
		builder.recommend(text(lang,
			"Review medium regressions and rerun the scenario to confirm stability.",
			"Проверьте средние регрессии и перезапустите сценарий, чтобы подтвердить стабильность.",
		))
	default:
		builder.add("ok", text(lang, "No regressions detected", "Регрессии не обнаружены"), text(lang,
			"No high or medium severity deltas were found by the current heuristic gate.",
			"Текущий эвристический порог не нашел изменений высокой или средней серьезности.",
		))
		builder.recommend(text(lang,
			"Keep the generated report with the build artifacts for future comparison.",
			"Сохраните отчет вместе с артефактами сборки для будущих сравнений.",
		))
	}

	return builder.finish()
}

type analysisBuilder struct {
	lang            string
	severity        string
	findings        []ReportFinding
	recommendations []string
}

func (b *analysisBuilder) add(severity, title, detail string) {
	if severityRank(severity) > severityRank(b.severity) {
		b.severity = severity
	}
	b.findings = append(b.findings, ReportFinding{Severity: severity, Title: title, Detail: detail})
}

func (b *analysisBuilder) recommend(value string) {
	for _, existing := range b.recommendations {
		if existing == value {
			return
		}
	}
	b.recommendations = append(b.recommendations, value)
}

func (b analysisBuilder) findingsWithoutCoverage() []ReportFinding {
	var out []ReportFinding
	for _, finding := range b.findings {
		if finding.Title != text(b.lang, "Coverage", "Покрытие") {
			out = append(out, finding)
		}
	}
	return out
}

func (b analysisBuilder) finish() ReportAnalysis {
	status := text(b.lang, "Healthy", "Все хорошо")
	summary := text(b.lang,
		"No serious performance problems were detected by the current heuristic thresholds.",
		"Текущие эвристические пороги не нашли серьезных перфоманс-проблем.",
	)
	switch b.severity {
	case "high":
		status = text(b.lang, "Serious issues detected", "Есть серьезные проблемы")
		summary = text(b.lang,
			"The report contains high-severity signals that should be investigated before treating this run as healthy.",
			"В отчете есть сигналы высокой серьезности; их нужно разобрать, прежде чем считать прогон здоровым.",
		)
	case "medium":
		status = text(b.lang, "Needs attention", "Требует внимания")
		summary = text(b.lang,
			"The report contains warning-level signals. The run may be acceptable for smoke testing, but it deserves review.",
			"В отчете есть предупреждающие сигналы. Для дымового теста это может быть приемлемо, но прогон стоит разобрать.",
		)
	}
	return ReportAnalysis{
		Severity:        b.severity,
		Status:          status,
		Summary:         summary,
		Findings:        b.findings,
		Recommendations: b.recommendations,
	}
}

func percentInt(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func severityRank(severity string) int {
	switch severity {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}

func text(lang, en, ru string) string {
	if lang == "ru" {
		return ru
	}
	return en
}

func textf(lang, en, ru string, args ...any) string {
	return fmt.Sprintf(text(lang, en, ru), args...)
}
