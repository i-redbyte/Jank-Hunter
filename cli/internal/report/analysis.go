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
	if summary.CollectionQuality.DiagnosticCompletenessLevel != "" && summary.CollectionQuality.DiagnosticCompletenessLevel != "excellent" {
		detail := textf(lang,
			"Diagnostic completeness is %s (%.2f%%).",
			"Диагностическая полнота: %s (%.2f%%).",
			diagnosticCompletenessLevelLabel(summary.CollectionQuality.DiagnosticCompletenessLevel),
			summary.CollectionQuality.DiagnosticCompletenessPercent,
		)
		if summary.CollectionQuality.DiagnosticCompletenessExplanation != "" {
			detail += " " + summary.CollectionQuality.DiagnosticCompletenessExplanation
		}
		detail += collectionQualityConsequence(summary.CollectionQuality)
		builder.add("medium", text(lang, "Data quality is limited", "Качество данных ограничено"), detail)
	}

	if summary.EventCount < 50 {
		builder.add("medium", text(lang, "Low sample size", "Малая выборка"), text(lang,
			"The run is small, so the verdict is useful as smoke-test feedback but not as a release-quality performance conclusion.",
			"Прогон небольшой, поэтому вердикт полезен как быстрый дымовой тест, но не как финальное заключение о производительности для релиза.",
		))
		builder.recommend(text(lang,
			"Collect several runs per scenario before trusting small deltas.",
			"Соберите несколько прогонов на сценарий, прежде чем доверять небольшим изменениям.",
		))
	}

	httpFailureRate := percentInt(summary.HTTPFailed, summary.HTTPCount)
	switch {
	case summary.HTTPCount > 0 && httpFailureRate >= 10:
		severity := sampleAwareSeverity("high", uint64(summary.HTTPCount), 5)
		builder.add(severity, text(lang, "HTTP failures are elevated", "Повышенный уровень HTTP-ошибок"), textf(lang,
			"%d of %d HTTP calls failed or returned 5xx (%.1f%%).",
			"%d из %d HTTP-вызовов завершились транспортной ошибкой или ответом 5xx (%.1f%%). При малом числе запросов это наблюдение нужно подтвердить повтором.",
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
	case summary.HTTPCount > 0 && summary.HTTPP95MS >= 1500:
		severity := sampleAwareSeverity("high", uint64(summary.HTTPCount), 5)
		builder.add(severity, text(lang, "HTTP p95 is very slow", "Зафиксирована высокая HTTP-задержка среди самых медленных запросов"), textf(lang,
			"HTTP p95 is %d ms across %d calls; confirm a small sample before treating it as a stable tail estimate.",
			"95%% запросов завершились не дольше чем за %d мс, всего записано %d запросов. На малой выборке это фактически одна из худших задержек, а не устойчивая оценка; подтвердите результат повтором.",
			summary.HTTPP95MS,
			summary.HTTPCount,
		))
		builder.recommend(text(lang,
			"Sort routes by p95 and TTFB; separate backend latency from DNS/connect/TLS overhead.",
			"Отсортируйте маршруты по границе верхних 5% задержек и времени до первого байта; отделите задержку сервера от поиска адреса, соединения и установки защищённого соединения.",
		))
	case summary.HTTPCount > 0 && summary.HTTPP95MS >= 700:
		builder.add("medium", text(lang, "HTTP p95 needs attention", "Задержка самых медленных HTTP-запросов требует внимания"), textf(lang,
			"HTTP p95 is %d ms.",
			"95%% запросов завершились не дольше чем за %d мс.",
			summary.HTTPP95MS,
		))
	}

	switch {
	case summary.UIFrames > 0 && summary.UIJankPct >= 10:
		severity := sampleAwareSeverity("high", summary.UIFrames, 120)
		builder.add(severity, text(lang, "UI jank is high", "Высокая доля подтормаживаний интерфейса"), textf(lang,
			"Janky frames are %.2f%% of %d observed frames. Treat a short UI window as preliminary evidence.",
			"Медленные кадры интерфейса составляют %.2f%% из %d наблюдаемых кадров. Для короткого интервала наблюдения вывод считается предварительным.",
			summary.UIJankPct,
			summary.UIFrames,
		))
		builder.recommend(text(lang,
			"Open the UI and owner sections together: main-thread stalls often explain the worst screen jank.",
			"Смотрите разделы интерфейса и источников вместе: паузы главного потока часто объясняют худшие подтормаживания экранов.",
		))
	case summary.UIFrames > 0 && summary.UIJankPct >= 3:
		builder.add("medium", text(lang, "UI jank is noticeable", "Подтормаживания интерфейса заметны"), textf(lang,
			"Janky frames are %.2f%% of all observed frames.",
			"Медленные кадры интерфейса составляют %.2f%% всех наблюдаемых кадров.",
			summary.UIJankPct,
		))
	}

	switch {
	case summary.UIFrames > 0 && summary.UIAvgFPS > 0 && summary.UIAvgFPS < 45:
		builder.add(sampleAwareSeverity("high", summary.UIFrames, 120), text(lang, "Average FPS is low", "Низкий средний FPS"), textf(lang,
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
			"Confirm the stall in the same operation and exclude heap-dump or diagnostic overhead before treating it as a release blocker.",
			"Повторите ту же операцию и исключите накладные расходы дампа памяти и диагностики, прежде чем считать паузу препятствием для выпуска.",
		))
	case summary.StallMaxMS >= 250:
		builder.add("medium", text(lang, "Main-thread stall detected", "Обнаружена пауза главного потока"), textf(lang,
			"Max observed stall is %d ms.",
			"Максимальная пауза главного потока = %d мс.",
			summary.StallMaxMS,
		))
	}

	if summary.LowMemoryCount > 0 {
		severity := "medium"
		if summary.LowMemoryCount >= 3 {
			severity = "high"
		}
		builder.add(severity, text(lang, "Low-memory samples present", "Есть сигналы низкой памяти"), textf(lang,
			"%d context samples reported low-memory state.",
			"%d снимков контекста сообщили системное состояние низкой памяти. Сам по себе этот сигнал не доказывает, что приложение вызвало давление на память.",
			summary.LowMemoryCount,
		))
		builder.recommend(text(lang,
			"Correlate low-memory samples with PSS growth and retained objects.",
			"Сопоставьте сигналы низкой памяти с ростом PSS и удержанными объектами.",
		))
	}
	if summary.Retained > 0 {
		severity := "medium"
		if hasHeapConfirmedRetention(summary) {
			severity = "high"
		}
		builder.add(severity, text(lang, "Retained objects detected", "Есть сигналы удержания объектов"), textf(lang,
			"Retained object count is %d.",
			"Событий удержания: %d. Наблюдение во время выполнения без пути из снимка кучи HPROF показывает неожиданно долгое удержание объекта, но не доказывает утечку памяти.",
			summary.Retained,
		))
		builder.recommend(text(lang,
			"Inspect retained classes and age buckets; old retained objects deserve priority.",
			"Проверьте удержанные классы и возрастные группы; старые удержанные объекты приоритетнее.",
		))
	}

	if len(builder.findingsWithoutCoverage()) == 0 {
		builder.add("ok", text(lang, "No serious issues confirmed", "Серьезные проблемы не подтверждены"), text(lang,
			"Available signals did not cross the heuristic thresholds. This conclusion applies only to the collected scenario and enabled telemetry.",
			"Доступные сигналы не пересекли эвристические пороги. Вывод относится только к записанному сценарию и включенным источникам телеметрии.",
		))
		builder.recommend(text(lang,
			"Use this report as a baseline and compare future runs against it.",
			"Используйте этот отчет как базу и сравнивайте с ним будущие прогоны.",
		))
	}

	return builder.finish()
}

func collectionQualityConsequence(quality analyze.CollectionQuality) string {
	if quality.Complete {
		return ""
	}
	return " Часть количественных оценок может быть занижена. Сохранённые места в коде и длительности пригодны для расследования, если рядом с конкретной проблемой не указано иное."
}

func compareAnalysis(comparison analyze.Comparison, lang string) ReportAnalysis {
	builder := analysisBuilder{lang: lang, severity: "ok"}
	high := 0
	medium := 0
	incomparable := 0
	for _, delta := range comparison.Deltas {
		if !delta.Comparable {
			incomparable++
			continue
		}
		switch delta.Severity {
		case "high":
			high++
			name := compareDeltaLabel(delta.Name)
			builder.add("high", textf(lang, "High regression: %s", "Высокая регрессия: %s", name), textf(lang,
				"%s -> %s (%s), confidence=%s, sample=%d.",
				"%s → %s (%s), доверие: %s, выборка: %d.",
				compareDeltaValue(delta.Baseline),
				compareDeltaValue(delta.Candidate),
				compareDeltaChange(delta.Change),
				confidenceLabel(delta.Confidence),
				delta.SampleSize,
			))
		case "medium":
			medium++
			name := compareDeltaLabel(delta.Name)
			builder.add("medium", textf(lang, "Medium regression: %s", "Средняя регрессия: %s", name), textf(lang,
				"%s -> %s (%s), confidence=%s, sample=%d.",
				"%s → %s (%s), доверие: %s, выборка: %d.",
				compareDeltaValue(delta.Baseline),
				compareDeltaValue(delta.Candidate),
				compareDeltaChange(delta.Change),
				confidenceLabel(delta.Confidence),
				delta.SampleSize,
			))
		}
	}

	for _, warning := range comparison.CohortWarnings {
		builder.add("medium", text(lang, "Cohort mismatch", "Несовпадение когорт"), warning)
	}
	for _, warning := range comparison.QualityWarnings {
		builder.add("medium", text(lang, "Data quality warning", "Ограничение качества данных"), warning)
	}
	for _, warning := range comparison.ExposureWarnings {
		builder.add("medium", text(lang, "Scenario duration differs", "Разная длительность сценариев"), warning)
	}
	if incomparable > 0 {
		builder.add("medium", text(lang, "Metrics without comparable data", "Есть метрики без сопоставимых данных"), textf(lang,
			"%d metrics were not compared because at least one run had no required measurements.",
			"Метрик без сравнения: %d. Хотя бы в одном прогоне отсутствовали необходимые измерения; нулевые значения не подставлялись.",
			incomparable,
		))
	}

	switch {
	case high > 0:
		builder.recommend(text(lang,
			"Investigate and reproduce high-severity deltas in the same scenario before release.",
			"До релиза разберите и повторите изменения высокой серьезности в том же сценарии.",
		))
	case medium > 0:
		builder.recommend(text(lang,
			"Review medium regressions and rerun the scenario to confirm stability.",
			"Проверьте средние регрессии и перезапустите сценарий, чтобы подтвердить стабильность.",
		))
	default:
		builder.add("ok", text(lang, "No regressions confirmed", "Регрессии не подтверждены"), text(lang,
			"Comparable metrics did not produce high or medium regression signals. Missing measurements and cohort warnings still limit the conclusion.",
			"Сопоставимые метрики не дали сигналов ухудшения высокой или средней серьезности. Отсутствующие измерения и предупреждения о когортах по-прежнему ограничивают вывод.",
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
	status := text(b.lang, "No serious issues confirmed", "Серьезные проблемы не подтверждены")
	summary := text(b.lang,
		"Available signals did not confirm serious performance problems within the collected scenario.",
		"Доступные сигналы не подтвердили серьезных проблем производительности в записанном сценарии.",
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

func sampleAwareSeverity(severity string, sample, minimum uint64) string {
	if sample < minimum && severity == "high" {
		return "medium"
	}
	return severity
}

func hasHeapConfirmedRetention(summary analyze.Summary) bool {
	for _, suspect := range summary.MemoryLeaks {
		if suspect.HeapEvidence && suspect.Severity == "high" {
			return true
		}
	}
	return false
}

func percentInt(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
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
