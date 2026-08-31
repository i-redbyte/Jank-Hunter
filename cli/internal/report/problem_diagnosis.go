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
	return "Проблемный сигнал зафиксирован, но данных для локализации первопричины пока недостаточно."
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
				"На экране %s медленными были %.1f%% кадров (%d из %d); p95 — %d мс, p99 — %d мс.",
				reportValue(item.Screen, "без атрибуции"), item.JankRatePct, item.JankyFrames, item.Frames, item.FrameP95MS, item.FrameP99MS,
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
					"Уровень связи «%s» описывает доступные данные. Вклад этого кандидата в конкретный медленный кадр нужно подтвердить общей временной трассой.",
					primary.Relation,
				),
				State: "boundary",
			},
		}
	}
	return []problemDiagnosisStep{
		{
			Label: "Кандидат",
			Text:  "Снимок стека локализует место, где главный поток находился во время паузы. Это точка входа в расследование, а не доказательство, что вся пауза потрачена в конечном методе стека.",
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
		if reportLocationHasApplicationSymbol(location) {
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

var reportFrameworkPrefixes = [...]string{
	"android.", "androidx.", "java.", "javax.", "kotlin.", "kotlinx.", "dalvik.",
	"libcore.", "sun.", "com.android.", "com.google.android.", "com.google.common.",
	"com.google.firebase.", "leakcanary.",
}

func reportLocationHasApplicationSymbol(location analyze.ProblemLocation) bool {
	for _, value := range []string{location.Class, location.Method, location.Owner} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		framework := false
		for _, prefix := range reportFrameworkPrefixes {
			if strings.HasPrefix(value, prefix) {
				framework = true
				break
			}
		}
		if !framework && strings.Contains(value, ".") {
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
		result = append(result, "Отдельных ограничений для этой находки не зафиксировано; всё равно подтвердите эффект повторным прогоном.")
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
		"ограниченные runtime-реестры",
		"writer отклонил",
		"качество сбора:",
		"часть событий не попала в журнал",
		"known lost",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func problemOrientedCollectionWarnings(summary analyze.Summary) []string {
	result := make([]string, 0, len(summary.Warnings))
	for _, warning := range summary.Warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" || isInternalCollectionLimitation(warning) {
			continue
		}
		result = appendUniqueReportText(result, warning)
	}
	return result
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
