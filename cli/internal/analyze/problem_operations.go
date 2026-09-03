package analyze

import (
	"fmt"
	"math"
)

func (b *problemBuilder) detectOperations() {
	analysis := b.summary.OperationAnalysis
	if analysis == nil {
		return
	}
	for _, operation := range analysis.Operations {
		failureCount := saturatingUint64Sum(operation.Failures, operation.Timeouts)
		failureRate := problemRatio(failureCount, operation.Count)
		breachRate := problemRatio(operation.BudgetBreaches, operation.Budgeted)
		budgetProblem := operation.Budgeted > 0 && operation.BudgetBreaches > 0
		failureProblem := failureCount > 0
		if !budgetProblem && !failureProblem {
			continue
		}

		impact := operationProblemImpact(operation.Kind)
		magnitude := 5
		if budgetProblem {
			magnitude = max(magnitude, operationRateMagnitude(breachRate, b.cfg.OperationBudgetBreachRate))
		}
		if failureProblem {
			magnitude = max(magnitude, operationRateMagnitude(failureRate, b.cfg.OperationFailureRate))
			impact = min(40, impact+5)
		}
		exposure := min(20, 3+int(math.Log2(float64(operation.Count)+1))*2)
		if operation.Count < b.cfg.OperationMinSample {
			exposure = min(exposure, 5)
		}
		correlatedKinds := boolCount(
			operation.CorrelatedStalls > 0,
			operation.CorrelatedHTTPFailures > 0,
			operation.CorrelatedUIJank > 0,
			operation.CorrelatedIO > 0,
			operation.CorrelatedProblems > 0,
			operation.CorrelatedRetainedObjects > 0,
		)
		compound := 0
		if correlatedKinds > 1 {
			compound = min(5, correlatedKinds)
		}

		confidence, reasons, limits := problemConfidence(
			b.summary,
			operation.Count,
			b.cfg.OperationMinSample,
			true,
		)
		if operationLifecycleIncomplete(analysis) {
			confidence = capProblemConfidence(confidence, "medium")
			reasons = append(reasons, "В наборе есть неполные или противоречивые цепочки операций.")
			limits = append(limits, "Неполный жизненный цикл может занижать частоту связанных сигналов и число завершений.")
		}
		if operationAggregationLimited(analysis) {
			confidence = "low"
			reasons = append(reasons, "Защитные пределы анализатора отбросили часть замеров операций.")
			limits = append(limits, "Полный охват операций не доказан из-за превышения пределов кардинальности.")
		}
		if operation.QuantilesApproximated {
			confidence = capProblemConfidence(confidence, "medium")
			reasons = append(reasons, "Границы длительности оценены потоковым алгоритмом с постоянным расходом памяти.")
			limits = append(limits, "После 128 завершений медиана и границы верхних 10% и 5% рассчитываются приближённо; максимум остаётся точным.")
		}

		name := displayUnknown(operation.Operation, "Операция без названия")
		title := fmt.Sprintf("%s выходит за заданный бюджет", name)
		subcategory := "budget"
		if failureProblem && budgetProblem {
			title = fmt.Sprintf("%s выходит за бюджет и завершается ошибками", name)
			subcategory = "budget_and_outcome"
		} else if failureProblem {
			title = fmt.Sprintf("%s завершается ошибками", name)
			subcategory = "outcome"
		}

		evidence := []ProblemEvidence{
			{Name: "Число завершений", Observed: fmt.Sprint(operation.Count), Unit: "events", Sample: u64ptr(operation.Count), Source: "operation_lifecycle"},
			{Name: medianDurationLabel, Observed: operationQuantileObserved(operation.P50MS, operation.QuantilesApproximated), Unit: "ms", Sample: u64ptr(operation.Count), Source: "operation_lifecycle"},
			{Name: upperTenDurationLabel, Observed: operationQuantileObserved(operation.P90MS, operation.QuantilesApproximated), Unit: "ms", Sample: u64ptr(operation.Count), Source: "operation_lifecycle"},
			{Name: upperFiveDurationLabel, Observed: operationQuantileObserved(operation.P95MS, operation.QuantilesApproximated), Unit: "ms", Sample: u64ptr(operation.Count), Source: "operation_lifecycle"},
			{Name: "Максимальная длительность", Observed: fmt.Sprint(operation.MaxMS), Unit: "ms", Sample: u64ptr(operation.Count), Source: "operation_lifecycle"},
		}
		if budgetProblem {
			evidence = append(evidence, ProblemEvidence{
				Name: "Нарушения бюджета", Observed: formatPercent(breachRate * 100), Unit: "%",
				ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.OperationBudgetBreachRate*100),
				Numerator:           u64ptr(operation.BudgetBreaches), Denominator: u64ptr(operation.Budgeted),
				Source: "operation_budget",
			})
		}
		if failureProblem {
			evidence = append(evidence, ProblemEvidence{
				Name: "Неуспешные завершения", Observed: formatPercent(failureRate * 100), Unit: "%",
				ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.OperationFailureRate*100),
				Numerator:           u64ptr(failureCount), Denominator: u64ptr(operation.Count),
				Source: "operation_outcome",
			})
		}

		factors := operationCorrelationFactors(operation)
		why := "Длительность, бюджет и итог записаны одной типизированной цепочкой операции. Связанные сигналы относятся к тому же идентификатору операции, но сами по себе не доказывают причину задержки."
		b.add(ProblemFinding{
			DetectorID: "operations.health", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryOperations, Subcategory: subcategory,
			Status: "observed", Confidence: confidence, ConfidenceReasons: uniqueStrings(reasons),
			Title: title,
			WhatHappened: fmt.Sprintf(
				"Из %d завершений: медиана %s мс, граница верхних 10%% — %s мс, граница верхних 5%% — %s мс, максимум %d мс; бюджет нарушен %d раз, неуспешных итогов %d.",
				operation.Count,
				operationQuantileObserved(operation.P50MS, operation.QuantilesApproximated),
				operationQuantileObserved(operation.P90MS, operation.QuantilesApproximated),
				operationQuantileObserved(operation.P95MS, operation.QuantilesApproximated), operation.MaxMS,
				operation.BudgetBreaches, failureCount,
			),
			Where:     []ProblemLocation{{Screen: operation.Screen, Operation: operation.Operation}},
			Why:       ProblemWhy{ClaimLevel: "linked", Summary: why, Factors: factors},
			Impact:    operationProblemImpactText(operation.Kind),
			Evidence:  evidence,
			Frequency: &ProblemFrequency{Count: operation.Count},
			Cost:      &ProblemCost{WallTimeMS: nonZeroU64Ptr(operation.TotalMS)},
			PriorityBreakdown: priority(
				impact, magnitude, exposure, 2, compound,
				"влияние вида операции", "доля нарушений бюджета и ошибок", "число завершений",
				"одна операция и экран", "совпавшие типизированные сигналы",
			),
			Recommendations: []ProblemRecommendation{{
				Action:       "Открыть почасовую таблицу, разрезы и этапы этой операции и начать с этапа с наибольшим вкладом",
				Rationale:    "Это отделяет общую просадку от конкретного времени, условия запуска и внутреннего этапа.",
				Verification: fmt.Sprintf("Повторить %s в тех же условиях не менее %d раз и сравнить границы верхних 10%% и 5%%, долю нарушений бюджета и ошибок.", name, b.cfg.OperationMinSample),
			}},
			Limitations: uniqueStrings(limits),
			Drilldowns:  []ProblemDrilldown{{Label: "Операции приложения", Anchor: "operations", Filter: operation.Operation}},
		})
	}
}

func operationQuantileObserved(value uint64, approximated bool) string {
	if approximated {
		return fmt.Sprintf("≈%d", value)
	}
	return fmt.Sprint(value)
}

func operationRateMagnitude(rate, recurringThreshold float64) int {
	if rate <= 0 {
		return 0
	}
	return min(25, 5+int(rate/recurringThreshold*5))
}

func operationProblemImpact(kind string) int {
	switch kind {
	case "user", "screen":
		return 30
	case "stage":
		return 24
	case "system":
		return 22
	default:
		return 18
	}
}

func operationProblemImpactText(kind string) []string {
	switch kind {
	case "user", "screen":
		return []string{"Пользователь дольше ждёт результат действия или готовность экрана"}
	case "stage":
		return []string{"Медленный или неуспешный этап увеличивает время родительской операции"}
	default:
		return []string{"Фоновая или системная работа опаздывает, повторяется либо не достигает результата"}
	}
}

func operationCorrelationFactors(operation OperationStats) []string {
	factors := make([]string, 0, 6)
	if operation.CorrelatedStalls > 0 {
		factors = append(factors, fmt.Sprintf("Внутри операции отмечено пауз главного потока: %d.", operation.CorrelatedStalls))
	}
	if operation.CorrelatedHTTPFailures > 0 {
		factors = append(factors, fmt.Sprintf("Внутри операции отмечено сетевых ошибок: %d.", operation.CorrelatedHTTPFailures))
	}
	if operation.CorrelatedUIJank > 0 {
		factors = append(factors, fmt.Sprintf("Внутри операции отмечено медленных кадров: %d.", operation.CorrelatedUIJank))
	}
	if operation.CorrelatedIO > 0 {
		factors = append(factors, fmt.Sprintf("Внутри операции отмечено операций ввода-вывода: %d.", operation.CorrelatedIO))
	}
	if operation.CorrelatedProblems > 0 {
		factors = append(factors, fmt.Sprintf("Внутри операции отмечено проблемных сигналов: %d.", operation.CorrelatedProblems))
	}
	if operation.CorrelatedRetainedObjects > 0 {
		factors = append(factors, fmt.Sprintf("Внутри операции отмечено удержанных объектов: %d.", operation.CorrelatedRetainedObjects))
	}
	return factors
}

func operationLifecycleIncomplete(analysis *OperationAnalysis) bool {
	return analysis.MissingFinish > 0 || analysis.MissingStart > 0 || analysis.DuplicateStart > 0 ||
		analysis.InconsistentLifecycle > 0 || analysis.MissingParent > 0
}

func operationAggregationLimited(analysis *OperationAnalysis) bool {
	return analysis.UnmatchedSignalEvents > 0 || analysis.DroppedActiveStarts > 0 ||
		analysis.DroppedSignalEvents > 0 || analysis.DroppedSignalRollups > 0 ||
		analysis.DroppedOperationSamples > 0 || analysis.DroppedTimeSlotSamples > 0 ||
		analysis.DroppedDimensionSamples > 0 || analysis.DroppedStageSamples > 0
}
