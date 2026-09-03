package analyze

import (
	"fmt"
	"sort"
)

func compareOperationAnalysis(baseline, candidate Summary) []OperationDelta {
	base := make(map[operationGroupKey]OperationStats)
	cand := make(map[operationGroupKey]OperationStats)
	keys := make(map[operationGroupKey]struct{})
	if baseline.OperationAnalysis != nil {
		for _, stats := range baseline.OperationAnalysis.Operations {
			key := operationGroupKey{name: stats.Operation, kind: stats.Kind, screen: stats.Screen}
			base[key] = stats
			keys[key] = struct{}{}
		}
	}
	if candidate.OperationAnalysis != nil {
		for _, stats := range candidate.OperationAnalysis.Operations {
			key := operationGroupKey{name: stats.Operation, kind: stats.Kind, screen: stats.Screen}
			cand[key] = stats
			keys[key] = struct{}{}
		}
	}
	rows := make([]OperationDelta, 0, len(keys))
	for key := range keys {
		before, hasBefore := base[key]
		after, hasAfter := cand[key]
		identity := before
		if !hasBefore {
			identity = after
		}
		row := OperationDelta{
			Operation: identity.Operation, Kind: identity.Kind, Screen: identity.Screen,
			BaselineCount: before.Count, CandidateCount: after.Count,
			BaselineP95MS: before.P95MS, CandidateP95MS: after.P95MS,
			BaselineQuantilesApproximated:  before.QuantilesApproximated,
			CandidateQuantilesApproximated: after.QuantilesApproximated,
			P95ChangeMS:                    float64(after.P95MS) - float64(before.P95MS),
			P95ChangePct:                   operationRelativeChange(before.P95MS, after.P95MS),
			BaselineBudgeted:               before.Budgeted,
			CandidateBudgeted:              after.Budgeted,
			BaselineBudgetBreachPct:        before.BudgetBreachRatePct,
			CandidateBudgetBreachPct:       after.BudgetBreachRatePct,
			BaselineFailureRatePct:         operationFailureRate(before),
			CandidateFailureRatePct:        operationFailureRate(after),
			BaselineDatabaseCallsPerOperation: databasePerOperation(
				float64(before.CorrelatedDatabase), before.Count,
			),
			CandidateDatabaseCallsPerOperation: databasePerOperation(
				float64(after.CorrelatedDatabase), after.Count,
			),
			BaselineDatabaseMainRatePct: databasePercent(
				before.CorrelatedDatabaseMain, before.CorrelatedDatabase,
			),
			CandidateDatabaseMainRatePct: databasePercent(
				after.CorrelatedDatabaseMain, after.CorrelatedDatabase,
			),
			BaselineDatabaseFailureRatePct: databasePercent(
				before.CorrelatedDatabaseErrors, before.CorrelatedDatabase,
			),
			CandidateDatabaseFailureRatePct: databasePercent(
				after.CorrelatedDatabaseErrors, after.CorrelatedDatabase,
			),
			BaselineDatabaseWallMSPerOperation: databasePerOperation(
				float64(before.CorrelatedDatabaseUS)/1_000, before.Count,
			),
			CandidateDatabaseWallMSPerOperation: databasePerOperation(
				float64(after.CorrelatedDatabaseUS)/1_000, after.Count,
			),
		}
		row.FailureRateChangePP = row.CandidateFailureRatePct - row.BaselineFailureRatePct
		switch {
		case !hasBefore:
			row.Status, row.Severity, row.Confidence = "new", "ok", "low"
			row.Note = "Операция отсутствует в базовом наборе; ухудшение не заявляется без сопоставимой выборки."
		case !hasAfter:
			row.Status, row.Severity, row.Confidence = "removed", "ok", "low"
			row.Note = "Операция отсутствует в новом наборе."
		default:
			minimum := minUint64(before.Count, after.Count)
			row.Confidence = operationComparisonConfidence(minimum, baseline, candidate)
			row.Comparable = minimum >= operationComparisonMinSample
			row.DatabaseComparable = row.Comparable &&
				before.CorrelatedDatabase > 0 && after.CorrelatedDatabase > 0
			row.BudgetComparable = row.Comparable &&
				minUint64(before.Budgeted, after.Budgeted) >= operationComparisonMinSample
			if row.BudgetComparable {
				row.BudgetBreachChangePP = after.BudgetBreachRatePct - before.BudgetBreachRatePct
			}
			if !row.Comparable {
				row.Status, row.Severity = "insufficient_data", "ok"
				row.Note = fmt.Sprintf(
					"Для описательного сравнения нужно минимум %d завершений в каждом наборе; сейчас минимум %d.",
					operationComparisonMinSample,
					minimum,
				)
			} else {
				row.Severity = operationDeltaSeverity(row)
				switch {
				case row.Severity == "high" || row.Severity == "medium" || row.Severity == "low":
					row.Status = "regressed"
				case row.P95ChangeMS < 0 || row.BudgetBreachChangePP < 0 || row.FailureRateChangePP < 0:
					row.Status = "improved"
				default:
					row.Status = "stable"
				}
				row.Note = "Описательное сравнение одинаковой операции; причинность требует одинаковых условий и достаточной выборки."
				if !row.BudgetComparable && (before.Budgeted > 0 || after.Budgeted > 0) {
					row.Note += fmt.Sprintf(
						" Изменение нарушений бюджета не учитывалось: нужно минимум %d операций с заданным бюджетом в каждом наборе; база %d, кандидат %d.",
						operationComparisonMinSample,
						before.Budgeted,
						after.Budgeted,
					)
				}
				if row.BaselineQuantilesApproximated || row.CandidateQuantilesApproximated {
					row.Confidence = lowerConfidenceLevel(row.Confidence, "medium")
					row.Note += " Граница 95% оценена потоковым алгоритмом с ограниченной памятью."
				}
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if operationSeverityRank(left.Severity) != operationSeverityRank(right.Severity) {
			return operationSeverityRank(left.Severity) > operationSeverityRank(right.Severity)
		}
		if left.P95ChangeMS != right.P95ChangeMS {
			return left.P95ChangeMS > right.P95ChangeMS
		}
		return operationGroupLess(
			operationGroupKey{name: left.Operation, kind: left.Kind, screen: left.Screen},
			operationGroupKey{name: right.Operation, kind: right.Kind, screen: right.Screen},
		)
	})
	return rows
}

func operationRelativeChange(before, after uint64) float64 {
	if before == 0 {
		return 0
	}
	return (float64(after) - float64(before)) * 100 / float64(before)
}

func operationFailureRate(stats OperationStats) float64 {
	if stats.Count == 0 {
		return 0
	}
	return float64(stats.Failures+stats.Timeouts) * 100 / float64(stats.Count)
}

func operationComparisonConfidence(samples uint64, baseline, candidate Summary) string {
	level := "low"
	if samples >= 100 {
		level = "high"
	} else if samples >= 20 {
		level = "medium"
	}
	return lowerConfidenceLevel(
		lowerConfidenceLevel(level, collectionConfidenceCap(baseline)),
		collectionConfidenceCap(candidate),
	)
}

func operationDeltaSeverity(row OperationDelta) string {
	if (row.P95ChangeMS >= 500 && row.P95ChangePct >= 50) || row.BudgetBreachChangePP >= 20 ||
		row.FailureRateChangePP >= 10 {
		return "high"
	}
	if (row.P95ChangeMS >= 200 && row.P95ChangePct >= 20) || row.BudgetBreachChangePP >= 10 ||
		row.FailureRateChangePP >= 5 {
		return "medium"
	}
	if row.P95ChangeMS > 0 || row.BudgetBreachChangePP > 0 || row.FailureRateChangePP > 0 {
		return "low"
	}
	return "ok"
}

func operationSeverityRank(value string) int {
	switch value {
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
