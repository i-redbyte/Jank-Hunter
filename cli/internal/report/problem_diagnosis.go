package report

import (
	"fmt"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/analyze"
)

type problemDiagnosisView struct {
	Verdict              string
	Proven               []string
	CausalChain          []problemDiagnosisStep
	Causes               []uiCauseInsight
	Locations            []problemDiagnosisLocation
	MissingProof         []string
	TechnicalLimitations []string
	Plan                 []problemDiagnosisPlanStep
}

type problemDiagnosisTemplateData struct {
	Finding   analyze.ProblemFinding
	Diagnosis problemDiagnosisView
}

type problemDiagnosisStep struct {
	Label string
	Text  string
	State string
}

type problemDiagnosisLocation struct {
	Scope string
	Text  string
}

type problemDiagnosisPlanStep struct {
	Number int
	Action string
}

type dataQualityAnalyzerNote struct {
	Area   string
	Detail string
}

func problemReportHeadline(summary analyze.ProblemSummary) string {
	if summary.Total > 0 && strings.TrimSpace(summary.Headline) != "" {
		return summary.Headline
	}
	return "В записанном сценарии явных проблем не обнаружено."
}

func problemReportVerdict(summary analyze.ProblemSummary) string {
	if summary.Total > 0 {
		return "problems_found"
	}
	return "clean"
}

func problemReportVerdictText(summary analyze.ProblemSummary) string {
	if summary.Total > 0 {
		return "Ниже - проблемы в порядке приоритета: сначала наиболее вредные для пользователя и приложения."
	}
	return "Вывод относится к записанному сценарию. Методика охвата и исходные счётчики доступны в подробном анализе."
}

func problemDiagnosis(summary analyze.Summary, finding analyze.ProblemFinding) problemDiagnosisView {
	screen := problemScreen(finding, summary.Problems)
	var causes []uiCauseInsight
	if problemUsesUICauses(finding) {
		causes = problemUICauses(summary, screen)
	}
	view := problemDiagnosisView{
		Verdict:              problemDiagnosisVerdict(finding, causes),
		Proven:               problemProvenEvidence(summary, finding, screen, causes),
		CausalChain:          problemCausalChain(finding, causes),
		Causes:               causes,
		Locations:            problemDiagnosisLocations(finding.Where),
		MissingProof:         problemMissingProof(finding),
		TechnicalLimitations: problemActionableLimitations(finding.Limitations),
		Plan:                 problemDiagnosisPlan(finding, causes),
	}
	return view
}

func problemUsesUICauses(finding analyze.ProblemFinding) bool {
	return problemHasUIContext(finding) ||
		finding.DetectorID == "stability.main_thread_stall" ||
		finding.DetectorID == "io.main_thread" ||
		finding.DetectorID == "io.room_main_thread"
}

func problemDiagnosisData(summary analyze.Summary, finding analyze.ProblemFinding) problemDiagnosisTemplateData {
	return problemDiagnosisTemplateData{Finding: finding, Diagnosis: problemDiagnosis(summary, finding)}
}

func problemDeltaDiagnosisData(comparison analyze.Comparison, delta analyze.ProblemDelta) problemDiagnosisTemplateData {
	if delta.Candidate != nil {
		return problemDiagnosisTemplateData{
			Finding:   *delta.Candidate,
			Diagnosis: problemDiagnosis(comparison.Candidate, *delta.Candidate),
		}
	}
	if delta.Baseline != nil {
		return problemDiagnosisTemplateData{
			Finding:   *delta.Baseline,
			Diagnosis: problemDiagnosis(comparison.Baseline, *delta.Baseline),
		}
	}
	return problemDiagnosisTemplateData{}
}

func problemScreen(finding analyze.ProblemFinding, raw []analyze.ProblemFinding) string {
	for _, location := range finding.Where {
		if value := strings.TrimSpace(location.Screen); value != "" {
			return value
		}
	}
	if len(finding.RelatedFindings) == 0 {
		return ""
	}
	related := make(map[string]struct{}, len(finding.RelatedFindings))
	for _, id := range finding.RelatedFindings {
		related[id] = struct{}{}
	}
	for _, item := range raw {
		if _, ok := related[item.ID]; !ok {
			continue
		}
		for _, location := range item.Where {
			if value := strings.TrimSpace(location.Screen); value != "" {
				return value
			}
		}
	}
	return ""
}

func problemUICauses(summary analyze.Summary, screen string) []uiCauseInsight {
	if screen == "" {
		return nil
	}
	semanticWork := analyze.ActionableSemanticWork(summary)
	for _, item := range summary.Screens {
		if sameKnownReportValue(item.Screen, screen) {
			return uiCauseInsights(summary, semanticWork, item)
		}
	}
	return mainThreadStallCauses(summary, screen)
}

func problemDiagnosisVerdict(finding analyze.ProblemFinding, causes []uiCauseInsight) string {
	if len(causes) > 0 {
		return fmt.Sprintf(
			"Первым проверяйте: %s. Это самый сильный маршрут расследования по собранным данным, но не окончательный диагноз: подтверждён сам сигнал, а полную причинную цепочку ещё нужно воспроизвести трассировкой.",
			causes[0].Title,
		)
	}
	if summary := strings.TrimSpace(finding.Why.Summary); summary != "" {
		return summary
	}
	if what := strings.TrimSpace(finding.WhatHappened); what != "" {
		return what
	}
	return "Проблемный сигнал есть, но данных недостаточно, чтобы точно назвать причину."
}

func problemProvenEvidence(
	summary analyze.Summary,
	finding analyze.ProblemFinding,
	screen string,
	causes []uiCauseInsight,
) []string {
	result := make([]string, 0, len(causes)+2)
	for _, cause := range causes {
		if cause.RelationClass == "direct" || cause.RelationClass == "strong" {
			result = appendUniqueReportText(result, cause.Evidence)
		}
	}
	if problemUsesUICauses(finding) {
		for _, item := range summary.Screens {
			if !sameKnownReportValue(item.Screen, screen) || item.Frames == 0 {
				continue
			}
			result = appendUniqueReportText(result, fmt.Sprintf(
				"На экране %s медленными были %.1f%% кадров (%d из %d); p95 - %d мс, p99 - %d мс.",
				reportValue(item.Screen, "экран не определён"), item.JankRatePct, item.JankyFrames, item.Frames, item.FrameP95MS, item.FrameP99MS,
			))
		}
	}
	if len(result) == 0 {
		result = appendUniqueReportText(result, strings.TrimSpace(finding.WhatHappened))
	}
	return result
}

func problemCausalChain(finding analyze.ProblemFinding, causes []uiCauseInsight) []problemDiagnosisStep {
	if len(causes) == 0 || !problemHasUIContext(finding) {
		return nil
	}
	hasStall := false
	for _, cause := range causes {
		if strings.HasPrefix(cause.stableKey, "stall\x00") {
			hasStall = true
			break
		}
	}
	if !hasStall {
		primary := causes[0]
		return []problemDiagnosisStep{
			{
				Label: "Наблюдение",
				Text:  primary.Evidence,
				State: "proven",
			},
			{
				Label: "Интерпретация",
				Text:  primary.Explanation,
				State: "hypothesis",
			},
			{
				Label: "Граница вывода",
				Text: fmt.Sprintf(
					"Уровень связи «%s» показывает силу доступных данных. Вклад этой причины в конкретный медленный кадр нужно проверить по общей временной трассе.",
					primary.Relation,
				),
				State: "boundary",
			},
		}
	}
	return []problemDiagnosisStep{
		{
			Label: "Место в коде",
			Text:  "Снимок стека показывает, где находился главный поток во время паузы. Начните проверку отсюда, но не считайте конечный метод причиной всей паузы без трассы.",
			State: "hypothesis",
		},
		{
			Label: "Подтверждено",
			Text:  "Главный поток действительно не выполнял обычную обработку во время измеренной паузы.",
			State: "proven",
		},
		{
			Label: "Механизм",
			Text:  "Пока главный поток занят или ждёт, он не может вовремя обработать ввод, измерение, размещение и отрисовку и подготовить следующий кадр.",
			State: "mechanism",
		},
		{
			Label: "Граница вывода",
			Text:  "Медленные кадры зарегистрированы на том же экране, но без общего идентификатора интервала или кадра это не доказывает совпадение по времени и не позволяет приписать все подтормаживания этой паузе.",
			State: "boundary",
		},
	}
}

func problemHasUIContext(finding analyze.ProblemFinding) bool {
	if finding.Category == analyze.ProblemCategoryUI {
		return true
	}
	for _, category := range finding.RelatedCategories {
		if category == analyze.ProblemCategoryUI {
			return true
		}
	}
	return false
}

func problemDiagnosisLocations(locations []analyze.ProblemLocation) []problemDiagnosisLocation {
	result := make([]problemDiagnosisLocation, 0, len(locations))
	for _, location := range locations {
		text := problemLocationText([]analyze.ProblemLocation{location})
		if text == "" || text == "место не определено" {
			continue
		}
		scope := "системный код / библиотека"
		if frameworkObservation := problemFrameworkObservationText(location); frameworkObservation != "" {
			scope = "точка наблюдения в библиотеке"
			text = frameworkObservation
		} else if reportLocationHasApplicationSymbol(location) {
			scope = "код приложения"
		}
		candidate := problemDiagnosisLocation{Scope: scope, Text: text}
		duplicate := false
		for _, existing := range result {
			if existing == candidate {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result
}

func reportLocationHasApplicationSymbol(location analyze.ProblemLocation) bool {
	for _, value := range []string{location.Class, location.Method, location.Owner} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !analyze.IsFrameworkSymbol(value) && strings.Contains(value, ".") {
			return true
		}
	}
	return false
}

func problemMissingProof(finding analyze.ProblemFinding) []string {
	result := make([]string, 0, len(finding.Limitations)+2)
	if problemHasUIContext(finding) && len(finding.RelatedFindings) > 1 {
		result = append(result, "Группировка по одному экрану не доказывает совпадение по времени: нужен общий интервал, идентификатор кадра или системная трасса.")
	}
	if finding.Why.ClaimLevel != "linked" && strings.TrimSpace(finding.Why.Summary) != "" {
		result = appendUniqueReportText(result, finding.Why.Summary)
	}
	for _, limitation := range problemActionableLimitations(finding.Limitations) {
		result = appendUniqueReportText(result, limitation)
	}
	if len(result) == 0 {
		result = append(result, "Отдельных ограничений для этой проблемы нет. Всё равно подтвердите результат повторным прогоном.")
	}
	return result
}

func problemActionableLimitations(limitations []string) []string {
	result := make([]string, 0, len(limitations))
	for _, limitation := range limitations {
		if isInternalCollectionLimitation(limitation) {
			continue
		}
		result = appendUniqueReportText(result, limitation)
	}
	return result
}

func isInternalCollectionLimitation(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range [...]string{
		"ограниченные runtime-" + "реестры",
		"реестры с ограниченным числом строк",
		"writer " + "отклонил",
		"модуль записи отклонил",
		"качество сбора:",
		"качество данных базового прогона:",
		"качество данных проверяемого прогона:",
		"часть событий не попала в журнал",
		"часть входных данных потеряна",
		"часть данных об удержании объектов или дампа памяти потеряна",
		"потеряно событий удержания",
		"события потеряны после",
		"метрики потеряны из-за ограничения",
		"вызовы графа потеряны после таймаута",
		"execute-вызовы потеряли sql-шаблон",
		"событие пропущено из-за конкуренции",
		"подтверждают потерю как минимум",
		"подтверждающих данных могла потеряться",
		"неполный жизненный цикл может занижать",
		"полный охват операций не доказан",
		"включён только основной процесс:",
		"список ожидаемых процессов объявлен не полностью",
		"ожидаемых процессов",
		"для восстановления цепочек binder не сохранено",
		"для восстановления цепочек binder пропущено",
		"known lost",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func dataQualityNotices(summary analyze.Summary) []string {
	result := make([]string, 0, len(summary.CollectionQuality.Notices)+len(summary.Warnings))
	for _, notice := range summary.CollectionQuality.Notices {
		result = appendUniqueReportText(result, notice)
	}
	for _, warning := range summary.Warnings {
		result = appendUniqueReportText(result, warning)
	}
	return result
}

func appendNonZeroDataQualityCount(details []string, label string, value uint64) []string {
	if value == 0 {
		return details
	}
	return append(details, fmt.Sprintf("%s: %d", label, value))
}

func dataQualityAnalyzerNotes(summary analyze.Summary) []dataQualityAnalyzerNote {
	result := make([]dataQualityAnalyzerNote, 0, 8)
	if components := summary.AndroidComponents; components != nil {
		if components.Partial && len(components.PartialReasons) > 0 {
			result = append(result, dataQualityAnalyzerNote{
				Area:   "Android-компоненты и IPC",
				Detail: strings.Join(components.PartialReasons, "; "),
			})
		}
		if components.Binder.UncorrelatableEvents > 0 || components.Binder.CorrelationDroppedEvents > 0 {
			details := make([]string, 0, 2)
			details = appendNonZeroDataQualityCount(details, "без данных для связи", components.Binder.UncorrelatableEvents)
			details = appendNonZeroDataQualityCount(details, "не сохранено связей", components.Binder.CorrelationDroppedEvents)
			result = append(result, dataQualityAnalyzerNote{
				Area:   "Связи Binder",
				Detail: strings.Join(details, ", "),
			})
		}
	}
	coverage := summary.DatabaseCoverage
	if coverage.DroppedStatementEvents > 0 || coverage.DroppedContextEvents > 0 ||
		coverage.DroppedCorrelationEvents > 0 || coverage.DroppedTransactionDetails > 0 ||
		coverage.DroppedScenarioEvents > 0 || coverage.DroppedScenarioCandidates > 0 {
		details := make([]string, 0, 6)
		details = appendNonZeroDataQualityCount(details, "SQL-событий", coverage.DroppedStatementEvents)
		details = appendNonZeroDataQualityCount(details, "контекстов", coverage.DroppedContextEvents)
		details = appendNonZeroDataQualityCount(details, "временных связей", coverage.DroppedCorrelationEvents)
		details = appendNonZeroDataQualityCount(details, "подробностей транзакций", coverage.DroppedTransactionDetails)
		details = appendNonZeroDataQualityCount(details, "событий сценариев", coverage.DroppedScenarioEvents)
		details = appendNonZeroDataQualityCount(details, "вариантов сценариев", coverage.DroppedScenarioCandidates)
		result = append(result, dataQualityAnalyzerNote{
			Area:   "Анализ базы данных",
			Detail: strings.Join(details, ", "),
		})
	}
	if database := summary.DatabaseAnalysis; database != nil {
		if database.FrequencyEstimateError > 0 || database.DroppedDBIntervals > 0 ||
			database.EvictedDBIntervals > 0 || database.DroppedTransactionIntervals > 0 ||
			database.EvictedTransactionIntervals > 0 || database.DroppedUIWindows > 0 ||
			database.EvictedUIWindows > 0 || database.DroppedStallIntervals > 0 ||
			database.EvictedStallIntervals > 0 || database.DroppedRelatedIntervals > 0 ||
			database.EvictedRelatedIntervals > 0 || database.EvictedStatementGroups > 0 ||
			database.EvictedContextGroups > 0 {
			details := make([]string, 0, 13)
			details = appendNonZeroDataQualityCount(details, "погрешность оценки частоты", database.FrequencyEstimateError)
			details = appendNonZeroDataQualityCount(details, "не сохранено интервалов БД", database.DroppedDBIntervals)
			details = appendNonZeroDataQualityCount(details, "вытеснено интервалов БД", database.EvictedDBIntervals)
			details = appendNonZeroDataQualityCount(details, "не сохранено интервалов транзакций", database.DroppedTransactionIntervals)
			details = appendNonZeroDataQualityCount(details, "вытеснено интервалов транзакций", database.EvictedTransactionIntervals)
			details = appendNonZeroDataQualityCount(details, "не сохранено окон UI", database.DroppedUIWindows)
			details = appendNonZeroDataQualityCount(details, "вытеснено окон UI", database.EvictedUIWindows)
			details = appendNonZeroDataQualityCount(details, "не сохранено пауз", database.DroppedStallIntervals)
			details = appendNonZeroDataQualityCount(details, "вытеснено пауз", database.EvictedStallIntervals)
			details = appendNonZeroDataQualityCount(details, "не сохранено связанных событий", database.DroppedRelatedIntervals)
			details = appendNonZeroDataQualityCount(details, "вытеснено связанных событий", database.EvictedRelatedIntervals)
			details = appendNonZeroDataQualityCount(details, "вытеснено групп SQL", database.EvictedStatementGroups)
			details = appendNonZeroDataQualityCount(details, "вытеснено групп контекста", database.EvictedContextGroups)
			result = append(result, dataQualityAnalyzerNote{
				Area:   "Ограничения потокового анализа БД",
				Detail: strings.Join(details, ", "),
			})
		}
	}
	if workers := summary.WorkerAnalysis; workers != nil &&
		(workers.MissingEnqueue > 0 || workers.MissingStart > 0 || workers.MissingFinish > 0) {
		details := make([]string, 0, 3)
		details = appendNonZeroDataQualityCount(details, "без времени постановки в очередь (не доказывает потери событий)", workers.MissingEnqueue)
		details = appendNonZeroDataQualityCount(details, "без запуска", workers.MissingStart)
		details = appendNonZeroDataQualityCount(details, "без завершения", workers.MissingFinish)
		result = append(result, dataQualityAnalyzerNote{
			Area:   "Жизненный цикл фоновых задач",
			Detail: strings.Join(details, ", "),
		})
	}
	if operations := summary.OperationAnalysis; operations != nil {
		if operations.MissingFinish > 0 || operations.MissingStart > 0 || operations.DuplicateStart > 0 ||
			operations.InconsistentLifecycle > 0 || operations.MissingParent > 0 {
			details := make([]string, 0, 5)
			details = appendNonZeroDataQualityCount(details, "без завершения", operations.MissingFinish)
			details = appendNonZeroDataQualityCount(details, "без начала", operations.MissingStart)
			details = appendNonZeroDataQualityCount(details, "повторных начал", operations.DuplicateStart)
			details = appendNonZeroDataQualityCount(details, "противоречивых завершений", operations.InconsistentLifecycle)
			details = appendNonZeroDataQualityCount(details, "без родительской операции", operations.MissingParent)
			result = append(result, dataQualityAnalyzerNote{
				Area:   "Целостность операций",
				Detail: strings.Join(details, ", "),
			})
		}
		if operations.DroppedActiveStarts > 0 || operations.DroppedSignalEvents > 0 ||
			operations.DroppedSignalRollups > 0 || operations.DroppedOperationSamples > 0 ||
			operations.DroppedTimeSlotSamples > 0 || operations.DroppedDimensionSamples > 0 ||
			operations.DroppedStageSamples > 0 || operations.CompletedContextEvictions > 0 {
			details := make([]string, 0, 8)
			details = appendNonZeroDataQualityCount(details, "не сохранено начал операций", operations.DroppedActiveStarts)
			details = appendNonZeroDataQualityCount(details, "не сохранено связанных сигналов", operations.DroppedSignalEvents)
			details = appendNonZeroDataQualityCount(details, "не сохранено связей с родителями", operations.DroppedSignalRollups)
			details = appendNonZeroDataQualityCount(details, "не сохранено общих замеров", operations.DroppedOperationSamples)
			details = appendNonZeroDataQualityCount(details, "не сохранено временных замеров", operations.DroppedTimeSlotSamples)
			details = appendNonZeroDataQualityCount(details, "не сохранено разрезов", operations.DroppedDimensionSamples)
			details = appendNonZeroDataQualityCount(details, "не сохранено этапов", operations.DroppedStageSamples)
			details = appendNonZeroDataQualityCount(details, "вытеснено контекстов завершённых операций", operations.CompletedContextEvictions)
			result = append(result, dataQualityAnalyzerNote{
				Area:   "Защитные лимиты анализа операций",
				Detail: strings.Join(details, ", "),
			})
		}
	}
	return result
}

func problemOrientedWarnings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if isInternalCollectionLimitation(value) {
			continue
		}
		result = appendUniqueReportText(result, value)
	}
	return result
}

func reportGenerationWarnings(values []string) []string {
	var result []string
	for _, value := range values {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "генерация отчета:") {
			continue
		}
		result = appendUniqueReportText(result, value)
	}
	return result
}

func conciseEvidenceConfidence(value string) string {
	value = strings.TrimSpace(value)
	if !strings.Contains(strings.ToLower(value), "потер") {
		return value
	}
	if label, _, ok := strings.Cut(value, ":"); ok {
		return strings.TrimSpace(label)
	}
	return "требует проверки"
}

func problemDiagnosisPlan(finding analyze.ProblemFinding, causes []uiCauseInsight) []problemDiagnosisPlanStep {
	actions := make([]string, 0, len(causes)+len(finding.Recommendations)+1)
	for _, cause := range causes {
		actions = appendUniqueReportText(actions, cause.Action)
	}
	for _, recommendation := range finding.Recommendations {
		actions = appendUniqueReportText(actions, recommendation.Action)
		if verification := strings.TrimSpace(recommendation.Verification); verification != "" {
			actions = appendUniqueReportText(actions, verification)
		}
	}
	if problemHasUIContext(finding) {
		actions = appendUniqueReportText(actions, "Повторите тот же пользовательский сценарий с Perfetto/System Trace и Jank Hunter; сопоставьте интервалы пауз с временной шкалой кадров, затем сравните максимум паузы, p95/p99 кадра и долю медленных кадров до и после изменения.")
	}
	result := make([]problemDiagnosisPlanStep, len(actions))
	for index, action := range actions {
		result[index] = problemDiagnosisPlanStep{Number: index + 1, Action: action}
	}
	return result
}

func appendUniqueReportText(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return values
	}
	for _, existing := range values {
		if existing == candidate {
			return values
		}
	}
	return append(values, candidate)
}

// Detailed HPROF parser diagnostics belong to mathematical data-quality.
func heapClassConfidence(value string) string {
	level, _, _ := strings.Cut(value, ":")
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "высокое", "высокая", "high":
		return "высокая"
	case "среднее", "среднее+", "средняя", "medium":
		return "средняя"
	case "низкое", "низкая", "low":
		return "низкая"
	default:
		return "не определена"
	}
}

func heapClassSize(heap analyze.HeapLeakEvidence) string {
	switch heap.RetainedSizeState {
	case analyze.HeapSizeExact:
		return humanDataSizeKB(heap.RetainedSizeKB)
	case analyze.HeapSizeEstimated:
		size := heap.RetainedSizeBytes
		if size == 0 {
			if heap.RetainedSizeKB > ^uint64(0)/1024 {
				return fmt.Sprintf("≥ %d КБ", heap.RetainedSizeKB)
			}
			size = heap.RetainedSizeKB * 1024
		}
		if size < 1024 {
			return fmt.Sprintf("≥ %d Б", size)
		}
		unit, label := uint64(1024), "КБ"
		if size >= 1<<30 {
			unit, label = 1<<30, "ГБ"
		} else if size >= 1<<20 {
			unit, label = 1<<20, "МБ"
		}
		// A lower bound must round down, never to a larger claimed number of bytes.
		return fmt.Sprintf("≥ %d.%d %s", size/unit, (size%unit)*10/unit, label)
	default:
		return "неизвестен"
	}
}

func heapInformation(summary analyze.Summary) []string {
	var items []string
	for _, d := range summary.HeapDiagnostics {
		if d.Informational() {
			items = append(items, d.Message)
		}
	}
	return items
}
