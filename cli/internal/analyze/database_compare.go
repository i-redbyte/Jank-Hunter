package analyze

import (
	"fmt"
	"sort"

	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const databaseComparisonMinSample = 20

type databaseComparisonKey struct {
	query             string
	operation         string
	fallbackSource    string
	fallbackFramework string
}

func compareDatabaseAnalysis(baseline, candidate Summary) DatabaseComparison {
	result := DatabaseComparison{}
	baseObserved := databaseSummaryObserved(baseline)
	candidateObserved := databaseSummaryObserved(candidate)
	result.Comparable = baseObserved && candidateObserved
	if !result.Comparable {
		result.Note = "DB call/transaction telemetry не зафиксирована хотя бы в одном прогоне; отсутствие измерения не подменяется нулём."
	} else if baseline.CollectorFlagsAll&uint64(jhlog.CollectorDatabase) == 0 ||
		candidate.CollectorFlagsAll&uint64(jhlog.CollectorDatabase) == 0 {
		result.Comparable = false
		result.Note = "DB collector включён не во всех сессиях хотя бы одного набора."
	}
	result.Metrics = databaseComparisonMetrics(baseline, candidate, result.Comparable)
	result.Statements = compareDatabaseStatements(baseline, candidate, result.Comparable)
	return result
}

func databaseSummaryObserved(summary Summary) bool {
	return summary.DatabaseAnalysis != nil &&
		(summary.DatabaseAnalysis.Overall.Calls > 0 || summary.DatabaseAnalysis.Transactions != nil &&
			summary.DatabaseAnalysis.Transactions.Events > 0)
}

func databaseComparisonMetrics(baseline, candidate Summary, setComparable bool) []Delta {
	base := databaseAnalysisOrZero(baseline)
	after := databaseAnalysisOrZero(candidate)
	baseOperations := databaseOperationExposure(baseline)
	afterOperations := databaseOperationExposure(candidate)
	metrics := []Delta{
		databaseLatencyDelta(
			"DB main-thread p95", base.Main, after.Main, setComparable,
		),
		databaseLatencyDelta(
			"DB background p95", base.Background, after.Background, setComparable,
		),
		databaseDurationRateDelta(
			"DB calls per minute", base.Overall.Calls, after.Overall.Calls,
			baseline.DurationMS, candidate.DurationMS,
			base.Overall.Calls, after.Overall.Calls, "выз./мин", setComparable,
		),
		databaseOperationRateDelta(
			"DB calls per operation", float64(base.Overall.Calls), float64(after.Overall.Calls),
			baseOperations, afterOperations, "выз./операцию", setComparable,
		),
		databasePercentageDelta(
			"DB main-thread rate", base.Main.Calls, after.Main.Calls,
			base.Overall.Calls, after.Overall.Calls, setComparable,
		),
		databasePercentageDelta(
			"DB failure rate", base.Overall.Failures, after.Overall.Failures,
			base.Overall.Calls, after.Overall.Calls, setComparable,
		),
		databasePercentageDelta(
			"DB rapid-repeat rate", base.RapidRepeats, after.RapidRepeats,
			base.Overall.Calls, after.Overall.Calls, setComparable,
		),
		databaseDurationRateDelta(
			"DB wall per minute", base.Overall.TotalDurationUS, after.Overall.TotalDurationUS,
			baseline.DurationMS, candidate.DurationMS,
			base.Overall.Calls, after.Overall.Calls, "мкс/мин", setComparable,
		),
		databaseOperationRateDelta(
			"DB wall per operation", float64(base.Overall.TotalDurationUS)/1_000,
			float64(after.Overall.TotalDurationUS)/1_000,
			baseOperations, afterOperations, "мс/операцию", setComparable,
		),
	}
	if base.Transactions != nil || after.Transactions != nil {
		baseTransactions := databaseTransactionsOrZero(base.Transactions)
		afterTransactions := databaseTransactionsOrZero(after.Transactions)
		metrics = append(metrics,
			databaseTransactionRateDelta(
				"DB transactions per minute", baseTransactions.Completed, afterTransactions.Completed,
				baseline.DurationMS, candidate.DurationMS, setComparable,
			),
			databaseTransactionLatencyDelta(baseTransactions, afterTransactions, setComparable),
			databaseTransactionPercentageDelta(
				"DB main-thread transaction rate",
				baseTransactions.MainThread, afterTransactions.MainThread,
				databaseTransactionInstances(baseTransactions), databaseTransactionInstances(afterTransactions),
				setComparable,
			),
			databaseTransactionPercentageDelta(
				"DB transaction failure rate",
				baseTransactions.Failures, afterTransactions.Failures,
				baseTransactions.Completed, afterTransactions.Completed,
				setComparable,
			),
			databaseTransactionPercentageDelta(
				"DB transaction rollback rate",
				baseTransactions.Rollbacks, afterTransactions.Rollbacks,
				baseTransactions.Completed, afterTransactions.Completed,
				setComparable,
			),
			databaseTransactionPercentageDelta(
				"DB incomplete transaction rate",
				baseTransactions.Incomplete, afterTransactions.Incomplete,
				baseTransactions.Begun, afterTransactions.Begun,
				setComparable,
			),
			databaseTransactionAverageStatementsDelta(baseTransactions, afterTransactions, setComparable),
		)
	}
	if base.Scenarios.ObservedOperationScopes > 0 || after.Scenarios.ObservedOperationScopes > 0 {
		metrics = append(metrics, databaseScenarioRateDelta(
			"DB repeated calls per operation scope",
			databaseScenarioCalls(base.Scenarios.Candidates, "operation", ""),
			databaseScenarioCalls(after.Scenarios.Candidates, "operation", ""),
			base.Scenarios.ObservedOperationScopes,
			after.Scenarios.ObservedOperationScopes,
			setComparable, databaseScenarioEvidenceComplete(base.Scenarios, after.Scenarios),
		))
	}
	if base.Scenarios.ObservedTransactionScopes > 0 || after.Scenarios.ObservedTransactionScopes > 0 {
		metrics = append(metrics,
			databaseScenarioRateDelta(
				"DB repeated calls per transaction scope",
				databaseScenarioCalls(base.Scenarios.Candidates, "transaction", ""),
				databaseScenarioCalls(after.Scenarios.Candidates, "transaction", ""),
				base.Scenarios.ObservedTransactionScopes,
				after.Scenarios.ObservedTransactionScopes,
				setComparable, databaseScenarioEvidenceComplete(base.Scenarios, after.Scenarios),
			),
			databaseScenarioRateDelta(
				"DB batch-candidate calls per transaction scope",
				databaseScenarioCalls(base.Scenarios.Candidates, "transaction", "batch_candidate"),
				databaseScenarioCalls(after.Scenarios.Candidates, "transaction", "batch_candidate"),
				base.Scenarios.ObservedTransactionScopes,
				after.Scenarios.ObservedTransactionScopes,
				setComparable, databaseScenarioEvidenceComplete(base.Scenarios, after.Scenarios),
			),
		)
	}
	comparisonConfidence := confidence(baseline, candidate)
	for index := range metrics {
		metrics[index].Confidence = comparisonConfidence
		metrics[index].Severity = adjustedSeverity(
			metrics[index].Severity,
			comparisonConfidence,
			metrics[index].SampleSize,
		)
	}
	return metrics
}

func databaseScenarioCalls(
	values []DatabaseScenarioStats,
	scopeKind, scenarioKind string,
) uint64 {
	var total uint64
	for _, value := range values {
		if value.ScopeKind != scopeKind || scenarioKind != "" && value.Kind != scenarioKind {
			continue
		}
		total = saturatingUint64Sum(total, value.EstimatedCalls)
	}
	return total
}

func databaseScenarioRateDelta(
	name string,
	baselineCalls, candidateCalls, baselineScopes, candidateScopes uint64,
	setComparable, evidenceComplete bool,
) Delta {
	result := relativeDeltaFloat(
		name,
		databasePerOperation(float64(baselineCalls), baselineScopes),
		databasePerOperation(float64(candidateCalls), candidateScopes),
		"выз./scope",
		true,
		minUint64(baselineScopes, candidateScopes),
	)
	if !setComparable {
		return databaseUnavailableDelta(
			result, baselineScopes, candidateScopes,
			"DB telemetry не зафиксирована хотя бы в одном прогоне",
		)
	}
	if baselineScopes == 0 || candidateScopes == 0 {
		return databaseUnavailableDelta(
			result, baselineScopes, candidateScopes,
			"operation/transaction scope identity не зафиксирована хотя бы в одном прогоне",
		)
	}
	if !evidenceComplete {
		return databaseUnavailableDelta(
			result, baselineScopes, candidateScopes,
			"bounded scenario evidence неполон хотя бы в одном прогоне",
		)
	}
	if minUint64(baselineScopes, candidateScopes) < databaseComparisonMinSample {
		return databaseUnavailableDelta(
			result, baselineScopes, candidateScopes,
			fmt.Sprintf("для scenario rate нужно не менее %d scopes в каждом прогоне", databaseComparisonMinSample),
		)
	}
	result.ComparisonNote = fmt.Sprintf(
		"Count-Min оценка повторов normalized fingerprint, нормировано по %d и %d наблюдаемым scopes; это hypothesis, не доказанный N+1",
		baselineScopes, candidateScopes,
	)
	return result
}

func databaseScenarioEvidenceComplete(values ...DatabaseScenarioAnalysis) bool {
	for _, value := range values {
		if value.DroppedEvents > 0 || value.DroppedCandidates > 0 {
			return false
		}
	}
	return true
}

func databaseTransactionsOrZero(value *DatabaseTransactionAnalysis) DatabaseTransactionAnalysis {
	if value == nil {
		return DatabaseTransactionAnalysis{}
	}
	return *value
}

func databaseTransactionInstances(value DatabaseTransactionAnalysis) uint64 {
	return saturatingUint64Sum(value.Completed, value.Incomplete)
}

func databaseTransactionLatencyDelta(
	baseline, candidate DatabaseTransactionAnalysis,
	setComparable bool,
) Delta {
	result := delta(
		"DB transaction p95", baseline.P95DurationUS, candidate.P95DurationUS, "мкс", true,
		minUint64(baseline.Completed, candidate.Completed),
	)
	if !setComparable || baseline.Completed == 0 || candidate.Completed == 0 {
		return databaseUnavailableDelta(
			result, baseline.Completed, candidate.Completed,
			"завершённые транзакции не зафиксированы хотя бы в одном прогоне",
		)
	}
	if minUint64(baseline.Completed, candidate.Completed) < databaseComparisonMinSample {
		return databaseUnavailableDelta(
			result, baseline.Completed, candidate.Completed,
			fmt.Sprintf("для transaction p95 нужно не менее %d завершений в каждом прогоне", databaseComparisonMinSample),
		)
	}
	result.ComparisonNote = "p95 только completed transaction lifecycle; incomplete не получают duration"
	return result
}

func databaseTransactionRateDelta(
	name string,
	baselineCount, candidateCount, baselineDurationMS, candidateDurationMS uint64,
	setComparable bool,
) Delta {
	result := relativeDeltaFloat(
		name,
		databasePerMinute(baselineCount, baselineDurationMS),
		databasePerMinute(candidateCount, candidateDurationMS),
		"txn/мин",
		true,
		minUint64(baselineCount, candidateCount),
	)
	if !setComparable || baselineCount == 0 || candidateCount == 0 {
		return databaseUnavailableDelta(
			result, baselineCount, candidateCount,
			"завершённые транзакции не зафиксированы хотя бы в одном прогоне",
		)
	}
	if baselineDurationMS == 0 || candidateDurationMS == 0 {
		return databaseUnavailableDelta(
			result, baselineDurationMS, candidateDurationMS,
			"длительность хотя бы одного прогона неизвестна",
		)
	}
	result.ComparisonNote = fmt.Sprintf(
		"completed transaction lifecycle, нормировано по %d и %d мс",
		baselineDurationMS, candidateDurationMS,
	)
	return result
}

func databaseTransactionPercentageDelta(
	name string,
	baselinePart, candidatePart, baselineTotal, candidateTotal uint64,
	setComparable bool,
) Delta {
	result := deltaFloat(
		name,
		databasePercent(baselinePart, baselineTotal),
		databasePercent(candidatePart, candidateTotal),
		"п.п.", true, minUint64(baselineTotal, candidateTotal),
	)
	if !setComparable || baselineTotal == 0 || candidateTotal == 0 {
		return databaseUnavailableDelta(
			result, baselineTotal, candidateTotal,
			"transaction lifecycle не зафиксирован хотя бы в одном прогоне",
		)
	}
	if minUint64(baselineTotal, candidateTotal) < databaseComparisonMinSample {
		return databaseUnavailableDelta(
			result, baselineTotal, candidateTotal,
			fmt.Sprintf("для transaction rate нужно не менее %d lifecycle instances в каждом прогоне", databaseComparisonMinSample),
		)
	}
	result.ComparisonNote = "сравнивается доля transaction lifecycle, а не сырое количество"
	return result
}

func databaseTransactionAverageStatementsDelta(
	baseline, candidate DatabaseTransactionAnalysis,
	setComparable bool,
) Delta {
	baselineAverage := databasePerOperation(float64(baseline.TotalStatementCount), baseline.Completed)
	candidateAverage := databasePerOperation(float64(candidate.TotalStatementCount), candidate.Completed)
	result := relativeDeltaFloat(
		"DB statements per transaction", baselineAverage, candidateAverage,
		"statement/txn", true, minUint64(baseline.Completed, candidate.Completed),
	)
	if !setComparable || baseline.Completed == 0 || candidate.Completed == 0 {
		return databaseUnavailableDelta(
			result, baseline.Completed, candidate.Completed,
			"завершённые транзакции не зафиксированы хотя бы в одном прогоне",
		)
	}
	if minUint64(baseline.Completed, candidate.Completed) < databaseComparisonMinSample {
		return databaseUnavailableDelta(
			result, baseline.Completed, candidate.Completed,
			fmt.Sprintf("для среднего fan-out нужно не менее %d завершений в каждом прогоне", databaseComparisonMinSample),
		)
	}
	result.ComparisonNote = "total statement count делится на completed transaction lifecycle"
	return result
}

func databaseAnalysisOrZero(summary Summary) DatabaseAnalysis {
	if summary.DatabaseAnalysis == nil {
		return DatabaseAnalysis{}
	}
	return *summary.DatabaseAnalysis
}

func databaseLatencyDelta(
	name string,
	baseline, candidate DatabaseExecutionStats,
	setComparable bool,
) Delta {
	result := delta(
		name,
		baseline.P95DurationUS,
		candidate.P95DurationUS,
		"мкс",
		true,
		minUint64(baseline.Calls, candidate.Calls),
	)
	if !setComparable || baseline.Calls == 0 || candidate.Calls == 0 {
		return databaseUnavailableDelta(result, baseline.Calls, candidate.Calls,
			"DB-вызовы этого класса потока не зафиксированы хотя бы в одном прогоне")
	}
	if minUint64(baseline.Calls, candidate.Calls) < databaseComparisonMinSample {
		return databaseUnavailableDelta(result, baseline.Calls, candidate.Calls,
			fmt.Sprintf("для p95 нужно не менее %d вызовов этого класса потока в каждом прогоне", databaseComparisonMinSample))
	}
	result.ComparisonNote = "thread-specific p95; main и background не смешиваются"
	return result
}

func databaseDurationRateDelta(
	name string,
	baselineValue, candidateValue, baselineDurationMS, candidateDurationMS uint64,
	baselineSamples, candidateSamples uint64,
	unit string,
	setComparable bool,
) Delta {
	baselineRate := databasePerMinute(baselineValue, baselineDurationMS)
	candidateRate := databasePerMinute(candidateValue, candidateDurationMS)
	result := relativeDeltaFloat(
		name,
		baselineRate,
		candidateRate,
		unit,
		true,
		minUint64(baselineSamples, candidateSamples),
	)
	if !setComparable || baselineSamples == 0 || candidateSamples == 0 {
		return databaseUnavailableDelta(result, baselineSamples, candidateSamples,
			"DB-вызовы не зафиксированы хотя бы в одном прогоне")
	}
	if baselineDurationMS == 0 || candidateDurationMS == 0 {
		return databaseUnavailableDelta(result, baselineDurationMS, candidateDurationMS,
			"длительность хотя бы одного прогона неизвестна")
	}
	result.ComparisonNote = fmt.Sprintf(
		"нормировано по длительности прогонов: %d и %d мс",
		baselineDurationMS,
		candidateDurationMS,
	)
	return result
}

func databaseOperationRateDelta(
	name string,
	baselineValue, candidateValue float64,
	baselineOperations, candidateOperations uint64,
	unit string,
	setComparable bool,
) Delta {
	baselineRate := databasePerOperation(baselineValue, baselineOperations)
	candidateRate := databasePerOperation(candidateValue, candidateOperations)
	result := relativeDeltaFloat(
		name,
		baselineRate,
		candidateRate,
		unit,
		true,
		minUint64(baselineOperations, candidateOperations),
	)
	if !setComparable {
		return databaseUnavailableDelta(result, 0, 0,
			"DB-вызовы не зафиксированы хотя бы в одном прогоне")
	}
	if minUint64(baselineOperations, candidateOperations) < databaseComparisonMinSample {
		return databaseUnavailableDelta(result, baselineOperations, candidateOperations,
			fmt.Sprintf("для нормализации нужно не менее %d завершённых операций приложения в каждом прогоне", databaseComparisonMinSample))
	}
	result.ComparisonNote = fmt.Sprintf(
		"нормировано по завершённым операциям приложения: %d и %d",
		baselineOperations,
		candidateOperations,
	)
	return result
}

func databasePercentageDelta(
	name string,
	baselinePart, candidatePart, baselineTotal, candidateTotal uint64,
	setComparable bool,
) Delta {
	result := deltaFloat(
		name,
		databasePercent(baselinePart, baselineTotal),
		databasePercent(candidatePart, candidateTotal),
		"п.п.",
		true,
		minUint64(baselineTotal, candidateTotal),
	)
	if !setComparable || baselineTotal == 0 || candidateTotal == 0 {
		return databaseUnavailableDelta(result, baselineTotal, candidateTotal,
			"DB-вызовы не зафиксированы хотя бы в одном прогоне")
	}
	if minUint64(baselineTotal, candidateTotal) < databaseComparisonMinSample {
		return databaseUnavailableDelta(result, baselineTotal, candidateTotal,
			fmt.Sprintf("для доли нужно не менее %d DB-вызовов в каждом прогоне", databaseComparisonMinSample))
	}
	result.ComparisonNote = "сравнивается доля от DB-вызовов, а не сырое количество"
	return result
}

func databaseUnavailableDelta(result Delta, baselineSamples, candidateSamples uint64, reason string) Delta {
	result = markDeltaUnavailable(result, baselineSamples, candidateSamples, reason)
	if baselineSamples == 0 {
		result.Baseline = "нет данных"
	}
	if candidateSamples == 0 {
		result.Candidate = "нет данных"
	}
	return result
}

func databaseOperationExposure(summary Summary) uint64 {
	if summary.OperationAnalysis == nil {
		return 0
	}
	return summary.OperationAnalysis.Completed
}

func databasePerMinute(value, durationMS uint64) float64 {
	if durationMS == 0 {
		return 0
	}
	return float64(value) * 60_000 / float64(durationMS)
}

func databasePerOperation(value float64, operations uint64) float64 {
	if operations == 0 {
		return 0
	}
	return value / float64(operations)
}

func databasePercent(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

func compareDatabaseStatements(
	baseline, candidate Summary,
	setComparable bool,
) []DatabaseStatementDelta {
	type statementPair struct {
		baseline, candidate *DatabaseStatementStats
	}
	baselineCount, candidateCount := databaseStatementCount(baseline.DatabaseAnalysis),
		databaseStatementCount(candidate.DatabaseAnalysis)
	pairs := make(map[databaseComparisonKey]statementPair, baselineCount+candidateCount)
	if baseline.DatabaseAnalysis != nil {
		for index := range baseline.DatabaseAnalysis.Statements {
			statement := &baseline.DatabaseAnalysis.Statements[index]
			key := databaseStatementComparisonKey(*statement)
			pair := pairs[key]
			pair.baseline = statement
			pairs[key] = pair
		}
	}
	if candidate.DatabaseAnalysis != nil {
		for index := range candidate.DatabaseAnalysis.Statements {
			statement := &candidate.DatabaseAnalysis.Statements[index]
			key := databaseStatementComparisonKey(*statement)
			pair := pairs[key]
			pair.candidate = statement
			pairs[key] = pair
		}
	}
	rows := make([]DatabaseStatementDelta, 0, len(pairs))
	comparisonConfidence := confidence(baseline, candidate)
	baselineOperations, candidateOperations := databaseOperationExposure(baseline),
		databaseOperationExposure(candidate)
	for _, pair := range pairs {
		var before, next DatabaseStatementStats
		hasBefore, hasAfter := pair.baseline != nil, pair.candidate != nil
		if hasBefore {
			before = *pair.baseline
		}
		if hasAfter {
			next = *pair.candidate
		}
		identity := before
		if !hasBefore {
			identity = next
		}
		row := DatabaseStatementDelta{
			Query: identity.Query, Operation: identity.Operation,
			BaselinePresent: hasBefore, CandidatePresent: hasAfter,
			BaselineCalls: before.Overall.Calls, CandidateCalls: next.Overall.Calls,
			BaselineCallsPerMinute:     databasePerMinute(before.Overall.Calls, baseline.DurationMS),
			CandidateCallsPerMinute:    databasePerMinute(next.Overall.Calls, candidate.DurationMS),
			BaselineCallsPerOperation:  databasePerOperation(float64(before.Overall.Calls), baselineOperations),
			CandidateCallsPerOperation: databasePerOperation(float64(next.Overall.Calls), candidateOperations),
			BaselineMainP95US:          before.Main.P95DurationUS, CandidateMainP95US: next.Main.P95DurationUS,
			BaselineBackgroundP95US:  before.Background.P95DurationUS,
			CandidateBackgroundP95US: next.Background.P95DurationUS,
			BaselineMaxUS:            before.Overall.MaxDurationUS, CandidateMaxUS: next.Overall.MaxDurationUS,
			BaselineMainRatePct:         databasePercent(before.Main.Calls, before.Overall.Calls),
			CandidateMainRatePct:        databasePercent(next.Main.Calls, next.Overall.Calls),
			BaselineFailureRatePct:      databasePercent(before.Overall.Failures, before.Overall.Calls),
			CandidateFailureRatePct:     databasePercent(next.Overall.Failures, next.Overall.Calls),
			BaselineRapidRepeatRatePct:  databasePercent(before.RapidRepeats, before.Overall.Calls),
			CandidateRapidRepeatRatePct: databasePercent(next.RapidRepeats, next.Overall.Calls),
			BaselineWallMSPerMinute:     databasePerMinute(before.Overall.TotalDurationUS, baseline.DurationMS) / 1_000,
			CandidateWallMSPerMinute:    databasePerMinute(next.Overall.TotalDurationUS, candidate.DurationMS) / 1_000,
			BaselineWallMSPerOperation:  databasePerOperation(float64(before.Overall.TotalDurationUS)/1_000, baselineOperations),
			CandidateWallMSPerOperation: databasePerOperation(float64(next.Overall.TotalDurationUS)/1_000, candidateOperations),
			Confidence:                  comparisonConfidence, Severity: "ok",
		}
		minimumCalls := minUint64(before.Overall.Calls, next.Overall.Calls)
		row.Comparable = setComparable && hasBefore && hasAfter && minimumCalls >= databaseComparisonMinSample
		row.LatencyComparable = row.Comparable &&
			(minUint64(before.Main.Calls, next.Main.Calls) >= databaseComparisonMinSample ||
				minUint64(before.Background.Calls, next.Background.Calls) >= databaseComparisonMinSample)
		row.ExposureComparable = row.Comparable && baseline.DurationMS > 0 && candidate.DurationMS > 0 &&
			minUint64(baselineOperations, candidateOperations) >= databaseComparisonMinSample
		row.Status, row.Note = databaseStatementComparisonStatus(row, minimumCalls)
		if row.Comparable {
			row.Severity = databaseStatementDeltaSeverity(
				row,
				before,
				next,
				baseline.DurationMS,
				candidate.DurationMS,
				baselineOperations,
				candidateOperations,
			)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) > severityRank(right.Severity)
		}
		if left.CandidateWallMSPerMinute != right.CandidateWallMSPerMinute {
			return left.CandidateWallMSPerMinute > right.CandidateWallMSPerMinute
		}
		if left.Query != right.Query {
			return left.Query < right.Query
		}
		return left.Operation < right.Operation
	})
	return rows
}

func databaseStatementCount(analysis *DatabaseAnalysis) int {
	if analysis == nil {
		return 0
	}
	return len(analysis.Statements)
}

func databaseStatementComparisonKey(statement DatabaseStatementStats) databaseComparisonKey {
	key := databaseComparisonKey{query: statement.Query, operation: statement.Operation}
	if isUnknownAnalysisValue(statement.Query) && len(statement.Contexts) > 0 {
		key.fallbackSource = statement.Contexts[0].Source
		key.fallbackFramework = statement.Contexts[0].Framework
	}
	return key
}

func databaseStatementComparisonStatus(
	row DatabaseStatementDelta,
	minimumCalls uint64,
) (string, string) {
	switch {
	case !row.BaselinePresent:
		return "new", "SQL отсутствует в базе; регрессия не заявляется без парной выборки."
	case !row.CandidatePresent:
		return "removed", "SQL отсутствует в кандидате."
	case minimumCalls < databaseComparisonMinSample:
		return "insufficient_data", "Для описательного сравнения нужно не менее 20 вызовов в каждом прогоне."
	case !row.Comparable:
		return "not_comparable", "Наборы DB telemetry несопоставимы по runtime coverage."
	default:
		return "compared", "Частота нормирована по длительности; latency сравнивается только по thread-specific выборкам."
	}
}

func databaseStatementDeltaSeverity(
	row DatabaseStatementDelta,
	baseline, candidate DatabaseStatementStats,
	baselineDurationMS, candidateDurationMS uint64,
	baselineOperations, candidateOperations uint64,
) string {
	minimumCalls := minUint64(baseline.Overall.Calls, candidate.Overall.Calls)
	severity := 0
	if minUint64(baseline.Main.Calls, candidate.Main.Calls) >= databaseComparisonMinSample {
		severity = max(severity, databaseRelativeSeverity(
			float64(baseline.Main.P95DurationUS), float64(candidate.Main.P95DurationUS),
		))
	}
	if minUint64(baseline.Background.Calls, candidate.Background.Calls) >= databaseComparisonMinSample {
		severity = max(severity, databaseRelativeSeverity(
			float64(baseline.Background.P95DurationUS), float64(candidate.Background.P95DurationUS),
		))
	}
	if minimumCalls >= databaseComparisonMinSample {
		severity = max(severity, databasePercentageSeverity(
			row.BaselineFailureRatePct, row.CandidateFailureRatePct,
		))
	}
	if baselineDurationMS > 0 && candidateDurationMS > 0 {
		severity = max(severity, databaseRelativeSeverity(
			row.BaselineCallsPerMinute, row.CandidateCallsPerMinute,
		))
		severity = max(severity, databaseRelativeSeverity(
			row.BaselineWallMSPerMinute, row.CandidateWallMSPerMinute,
		))
	}
	if minUint64(baselineOperations, candidateOperations) >= databaseComparisonMinSample {
		severity = max(severity, databaseRelativeSeverity(
			row.BaselineCallsPerOperation, row.CandidateCallsPerOperation,
		))
		severity = max(severity, databaseRelativeSeverity(
			row.BaselineWallMSPerOperation, row.CandidateWallMSPerOperation,
		))
	}
	if row.Confidence == "low" && severity == severityRank("high") {
		severity = severityRank("medium")
	}
	switch severity {
	case 3:
		return "high"
	case 2:
		return "medium"
	default:
		return "ok"
	}
}

func databaseRelativeSeverity(baseline, candidate float64) int {
	if baseline == 0 {
		if candidate > 0 {
			return severityRank("medium")
		}
		return 0
	}
	regressionPct := (candidate - baseline) * 100 / baseline
	switch {
	case regressionPct >= 25:
		return severityRank("high")
	case regressionPct >= 10:
		return severityRank("medium")
	default:
		return 0
	}
}

func databasePercentageSeverity(baseline, candidate float64) int {
	difference := candidate - baseline
	switch {
	case difference >= 3:
		return severityRank("high")
	case difference >= 1:
		return severityRank("medium")
	default:
		return 0
	}
}
