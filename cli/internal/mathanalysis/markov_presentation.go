package mathanalysis

import "fmt"

func markovConfidenceLabel(value string) string {
	switch value {
	case "high":
		return "высокая"
	case "medium":
		return "средняя"
	case "low":
		return "низкая"
	default:
		return "неизвестная"
	}
}

func MarkovConfidenceLabel(value string) string {
	return markovConfidenceLabel(value)
}

func markovStatus(model MarkovModel) string {
	if len(model.States) < 2 {
		return "medium"
	}
	if model.HealthyToBadCount > 0 && model.HasRecoveryProbability && model.BadToHealthyProbability < 0.35 {
		return "high"
	}
	for _, sticky := range model.StickyStates {
		if markovIsBadState(sticky.State) && sticky.Probability >= 0.70 {
			return "high"
		}
	}
	if model.Confidence == "low" {
		return "medium"
	}
	if model.HealthyToBadCount > 0 || model.ExpectedRecoveryWindows >= 3 {
		return "medium"
	}
	return "ok"
}

func markovSummary(model MarkovModel) string {
	if len(model.States) < 2 {
		return "Недостаточно временных интервалов для матрицы переходов состояний."
	}
	if normalizedRunCount(model.IndependentRunCount) > 1 {
		return fmt.Sprintf("Независимых прогонов: %d; относительных временных интервалов: %d. Состояния описывают общий профиль сценария; прогноз и выводы о хронологической траектории отключены.", normalizedRunCount(model.IndependentRunCount), len(model.States))
	}
	recovery := "завершённое восстановление не наблюдалось"
	if model.HasRecoveryProbability {
		recovery = fmt.Sprintf("восстановление %.1f%%", model.BadToHealthyProbability*100)
	}
	if model.HasExpectedRecovery {
		recovery += fmt.Sprintf(" за %.1f интервала / %.0f мс", model.ExpectedRecoveryWindows, model.ExpectedRecoveryMS)
	}
	return fmt.Sprintf("Измерено %d из %d временных интервалов, переходов=%d, типов переходов=%d. Плохие состояния занимали %.1f%% измеренного времени; входов из нормального состояния — %d; %s. Уверенность %s.", len(model.States), model.TimelineBucketCount, model.TransitionEventCount, len(model.Transitions), model.BadStateExposure*100, model.HealthyToBadCount, recovery, markovConfidenceLabel(model.Confidence))
}

func markovFindings(model MarkovModel) []Finding {
	if len(model.States) < 2 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Недостаточно данных для марковской модели",
			Detail:         markovSummary(model),
			Recommendation: "Нужны хотя бы два временных интервала сценария.",
		}}
	}
	if normalizedRunCount(model.IndependentRunCount) > 1 {
		return []Finding{{
			Severity:       "medium",
			Title:          "Марковская последовательность объединяет разные прогоны",
			Detail:         markovSummary(model),
			Recommendation: "Откройте отдельный прогон, если нужен прогноз или анализ переходов по времени. В объединённом отчёте используйте состояния только как профиль проблем по позиции сценария.",
		}}
	}
	if model.BadStateExposure > 0 && !model.HasRecoveryProbability {
		return []Finding{{
			Severity:       "medium",
			Title:          "Восстановление после плохого состояния не наблюдалось",
			Detail:         markovSummary(model),
			Recommendation: "Продлите этот одиночный прогон до спокойного состояния или завершения сценария. Ноль в метриках восстановления здесь не означает мгновенное восстановление.",
		}}
	}
	if model.Confidence == "low" {
		return []Finding{{
			Severity:       "medium",
			Title:          "Низкая уверенность марковской модели",
			Detail:         model.ConfidenceReason + ". " + markovSummary(model),
			Recommendation: "Соберите более длинный одиночный прогон. Повторы анализируйте отдельно или сопоставляйте попарно, чтобы не смешивать последовательности состояний.",
		}}
	}
	if model.HealthyToBadCount > 0 && model.HasRecoveryProbability && model.BadToHealthyProbability < 0.5 {
		return []Finding{{
			Severity:       markovStatus(model),
			Title:          "Слабое восстановление после плохих состояний",
			Detail:         markovSummary(model),
			Recommendation: "Проверьте причины плохих окон: сетевые циклы, точки изменения, интегральную нагрузку и источники работ.",
		}}
	}
	for _, sticky := range model.StickyStates {
		if markovIsBadState(sticky.State) && sticky.Probability >= 0.5 {
			return []Finding{{
				Severity:       markovStatus(model),
				Title:          "Найдено липкое плохое состояние",
				Detail:         fmt.Sprintf("%s повторяется само в себя с вероятностью %.1f%%.", MarkovStateLabel(sticky.State), sticky.Probability*100),
				Recommendation: "Посмотрите соседние временные интервалы, место запуска и маршрут: повторение плохого состояния обычно означает повторяющуюся работу или отсутствие задержки повторов.",
			}}
		}
	}
	for _, sticky := range model.ContextStickyStates {
		if markovIsBadState(sticky.State) && sticky.Probability >= 0.5 {
			return []Finding{{
				Severity:       markovStatus(model),
				Title:          "Липкое плохое состояние привязано к контексту",
				Detail:         fmt.Sprintf("%s повторяется в контексте %s с вероятностью %.1f%%.", MarkovStateLabel(sticky.State), sticky.Context, sticky.Probability*100),
				Recommendation: "Проверьте место запуска, маршрут и экран рядом с этим контекстом: повторение плохого состояния часто указывает на повторяющуюся работу без задержки повторов или очистки.",
			}}
		}
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Марковская модель не показывает опасных переходов",
		Detail:   markovSummary(model),
	}}
}

func compareMarkovSummary(deltas []MarkovDelta) string {
	if len(deltas) == 0 {
		return "Недостаточно марковских метрик для сравнения."
	}
	var worse int
	var incomparable int
	for _, delta := range deltas {
		if !delta.Comparable {
			incomparable++
			continue
		}
		if delta.Severity == "high" || delta.Severity == "medium" {
			worse++
		}
	}
	if incomparable > 0 {
		return fmt.Sprintf("Сопоставимо %d из %d марковских метрик; для %d метрик нет одинаковой наблюдаемой основы. Среди сопоставимых ухудшено %d.", len(deltas)-incomparable, len(deltas), incomparable, worse)
	}
	if worse == 0 {
		return "Проверяемый прогон не ухудшил переходы между состояниями."
	}
	return fmt.Sprintf("Проверяемый прогон ухудшил %d марковских метрик из %d.", worse, len(deltas))
}

func compareMarkovFindings(deltas []MarkovDelta) []Finding {
	if len(deltas) == 0 {
		return []Finding{{
			Severity: "medium",
			Title:    "Марковское сравнение недоступно",
			Detail:   compareMarkovSummary(deltas),
		}}
	}
	var findings []Finding
	for _, delta := range deltas {
		if !delta.Comparable {
			findings = append(findings, Finding{
				Severity:       "medium",
				Title:          "Часть марковского сравнения недоступна",
				Detail:         delta.Summary,
				Recommendation: "Сравните по одному независимому прогону одинакового сценария, сопоставляйте повторы попарно и дождитесь завершённого плохого эпизода на обеих сторонах.",
			})
			break
		}
	}
	var worst *MarkovDelta
	for _, delta := range deltas {
		if !delta.Comparable || (delta.Severity != "high" && delta.Severity != "medium") {
			continue
		}
		if worst == nil || severityRank(delta.Severity) > severityRank(worst.Severity) {
			item := delta
			worst = &item
		}
	}
	if worst != nil {
		findings = append(findings, Finding{
			Severity:       worst.Severity,
			Title:          "Изменились переходы состояний",
			Detail:         worst.Summary,
			Recommendation: "Сравните последовательность состояний проверяемого прогона с временной шкалой, сетевыми циклами и накопленной нагрузкой.",
		})
	}
	if len(findings) > 0 {
		return findings
	}
	return []Finding{{
		Severity: "ok",
		Title:    "Регрессий марковских переходов не найдено",
		Detail:   compareMarkovSummary(deltas),
	}}
}
