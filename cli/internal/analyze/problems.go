package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/i-redbyte/jank-hunter/cli/internal/datavalue"
	"github.com/i-redbyte/jank-hunter/cli/internal/jhlog"
)

const ProblemSchemaVersion = "jankhunter.problems/v2"

const (
	ProblemCategoryStability           = "stability"
	ProblemCategoryOperations          = "operations"
	ProblemCategoryUI                  = "ui_main_thread"
	ProblemCategoryNetwork             = "network"
	ProblemCategoryMemory              = "memory_gc"
	ProblemCategoryIO                  = "io_storage"
	ProblemCategoryCPU                 = "cpu_tasks"
	ProblemCategoryPower               = "power_thermal"
	ProblemCategoryLogs                = "logs"
	ProblemCategoryAndroidComponents   = "android_components_ipc"
	ProblemCategoryDependencyInjection = "dependency_injection"

	medianDurationLabel    = "Медиана длительности"
	upperTenDurationLabel  = "Граница верхних 10% длительности"
	upperFiveDurationLabel = "Граница верхних 5% длительности"
)

type ProblemFinding struct {
	ID                    string                     `json:"id"`
	Fingerprint           string                     `json:"fingerprint"`
	DetectorID            string                     `json:"detector_id"`
	DetectorVersion       string                     `json:"detector_version"`
	Category              string                     `json:"category"`
	Subcategory           string                     `json:"subcategory"`
	Severity              string                     `json:"severity"`
	Status                string                     `json:"status"`
	InvestigationPriority int                        `json:"investigation_priority"`
	Confidence            string                     `json:"confidence"`
	ConfidenceReasons     []string                   `json:"confidence_reasons"`
	Title                 string                     `json:"title"`
	WhatHappened          string                     `json:"what_happened"`
	Where                 []ProblemLocation          `json:"where"`
	Why                   ProblemWhy                 `json:"why"`
	Impact                []string                   `json:"impact"`
	Evidence              []ProblemEvidence          `json:"evidence"`
	Frequency             *ProblemFrequency          `json:"frequency,omitempty"`
	Cost                  *ProblemCost               `json:"cost,omitempty"`
	PriorityBreakdown     []ProblemPriorityComponent `json:"priority_breakdown"`
	Recommendations       []ProblemRecommendation    `json:"recommendations"`
	Limitations           []string                   `json:"limitations,omitempty"`
	Drilldowns            []ProblemDrilldown         `json:"drilldowns,omitempty"`
	RelatedFindings       []string                   `json:"related_findings,omitempty"`
	RelatedCategories     []string                   `json:"related_categories,omitempty"`
}

type ProblemLocation struct {
	Process   string `json:"process,omitempty"`
	Screen    string `json:"screen,omitempty"`
	Operation string `json:"operation,omitempty"`
	Route     string `json:"route,omitempty"`
	Owner     string `json:"owner,omitempty"`
	Class     string `json:"class,omitempty"`
	Method    string `json:"method,omitempty"`
}

type ProblemWhy struct {
	ClaimLevel string   `json:"claim_level"`
	Summary    string   `json:"summary"`
	Factors    []string `json:"factors,omitempty"`
}

type ProblemEvidence struct {
	Name                string  `json:"name"`
	Observed            string  `json:"observed"`
	Unit                string  `json:"unit,omitempty"`
	ExpectedOrThreshold string  `json:"expected_or_threshold,omitempty"`
	Sample              *uint64 `json:"sample,omitempty"`
	Numerator           *uint64 `json:"numerator,omitempty"`
	Denominator         *uint64 `json:"denominator,omitempty"`
	Source              string  `json:"source"`
}

type ProblemFrequency struct {
	Count       uint64   `json:"count"`
	RatePerSec  *float64 `json:"rate_per_sec,omitempty"`
	RunCoverage *float64 `json:"run_coverage,omitempty"`
}

type ProblemCost struct {
	WallTimeMS          *uint64 `json:"wall_time_ms,omitempty"`
	MainThreadBlockedMS *uint64 `json:"main_thread_blocked_ms,omitempty"`
	Bytes               *uint64 `json:"bytes,omitempty"`
	MemoryKB            *uint64 `json:"memory_kb,omitempty"`
}

type ProblemPriorityComponent struct {
	Component   string `json:"component"`
	Score       int    `json:"score"`
	Maximum     int    `json:"maximum"`
	Explanation string `json:"explanation"`
}

type ProblemRecommendation struct {
	Action       string `json:"action"`
	Rationale    string `json:"rationale"`
	Verification string `json:"verification"`
}

type ProblemDrilldown struct {
	Label  string `json:"label"`
	Anchor string `json:"anchor"`
	Filter string `json:"filter,omitempty"`
}

type CategoryCoverage struct {
	Category          string   `json:"category"`
	Label             string   `json:"label"`
	Status            string   `json:"status"`
	WorstSeverity     string   `json:"worst_severity,omitempty"`
	FindingCount      int      `json:"finding_count"`
	RequiredEvidence  []string `json:"required_evidence"`
	AvailableEvidence []string `json:"available_evidence,omitempty"`
	MissingEvidence   []string `json:"missing_evidence,omitempty"`
	Explanation       string   `json:"explanation"`
	NextAction        string   `json:"next_action,omitempty"`
}

type ProblemSummary struct {
	SchemaVersion string         `json:"schema_version"`
	Verdict       string         `json:"verdict"`
	Headline      string         `json:"headline"`
	Total         int            `json:"total"`
	SignalTotal   int            `json:"signal_total"`
	Critical      int            `json:"critical"`
	High          int            `json:"high"`
	Medium        int            `json:"medium"`
	Low           int            `json:"low"`
	Info          int            `json:"info"`
	ByCategory    []ProblemCount `json:"by_category"`
	Unchecked     int            `json:"unchecked_categories"`
}

type ProblemCount struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type DetectorMetadata struct {
	ID              string              `json:"id"`
	Version         string              `json:"version"`
	Category        string              `json:"category"`
	Title           string              `json:"title"`
	MinimumSample   uint64              `json:"minimum_sample"`
	RequiredSignals []string            `json:"required_signals"`
	Thresholds      []DetectorThreshold `json:"thresholds"`
}

type DetectorThreshold struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type ProblemDetectorConfig struct {
	Version                         string
	HTTPSlowMS                      uint64
	HTTPHighMS                      uint64
	HTTPMinSample                   uint64
	HTTPFailureRate                 float64
	HTTPStormRate                   float64
	HTTPStormMinCount               uint64
	UIJankRate                      float64
	UIHighJankRate                  float64
	UIMinFrames                     uint64
	UIFrameTailMS                   uint64
	StallMS                         uint64
	StallHighMS                     uint64
	IOMainThreadMS                  uint64
	IOBackgroundMS                  uint64
	IOStormMinCount                 uint64
	IOStormRate                     float64
	IOFailureMinCount               uint64
	IOFailureRate                   float64
	IOLargeMainBytes                uint64
	IOLargeBackgroundBytes          uint64
	IOSmallOperationBytes           uint64
	DatabaseMainThreadMS            uint64
	DatabaseBackgroundMS            uint64
	DatabaseStormMinCount           uint64
	DatabaseStormRate               uint64
	DatabaseRapidRepeatCount        uint64
	DatabaseFailureRate             float64
	DatabaseTransactionMainMS       uint64
	DatabaseTransactionBackgroundMS uint64
	DatabaseTransactionStatements   uint64
	LogSpamMinCount                 uint64
	LogSpamRate                     float64
	ProcessCPUPercent               float64
	ThermalSevereStatus             uint64
	AsyncQueueWaitMS                uint64
	AsyncQueueMinSamples            uint64
	GCBlockingTimeMS                uint64
	StartupColdResumeMS             uint64
	ScreenResumeMS                  uint64
	OperationMinSample              uint64
	OperationBudgetBreachRate       float64
	OperationFailureRate            float64
}

func DefaultProblemDetectorConfig() ProblemDetectorConfig {
	return ProblemDetectorConfig{
		Version: "2.0.0", HTTPSlowMS: 700, HTTPHighMS: 1500, HTTPMinSample: 20,
		HTTPFailureRate: 0.05, HTTPStormRate: 1, HTTPStormMinCount: 20,
		UIJankRate: 3, UIHighJankRate: 10, UIMinFrames: 120, UIFrameTailMS: 32,
		StallMS: 250, StallHighMS: 1000, IOMainThreadMS: 16, IOBackgroundMS: 500,
		IOStormMinCount: 50, IOStormRate: 5, IOFailureMinCount: 2, IOFailureRate: 0.25,
		IOLargeMainBytes: 1 << 20, IOLargeBackgroundBytes: 16 << 20, IOSmallOperationBytes: 64 << 10,
		DatabaseMainThreadMS: 16, DatabaseBackgroundMS: 100,
		DatabaseStormMinCount: 20, DatabaseStormRate: 10, DatabaseRapidRepeatCount: 5,
		DatabaseFailureRate:       0.10,
		DatabaseTransactionMainMS: 16, DatabaseTransactionBackgroundMS: 100,
		DatabaseTransactionStatements: 20,
		LogSpamMinCount:               100, LogSpamRate: 5,
		ProcessCPUPercent: 80, ThermalSevereStatus: 3,
		AsyncQueueWaitMS: 200, AsyncQueueMinSamples: 3, GCBlockingTimeMS: 50,
		StartupColdResumeMS: 2_000, ScreenResumeMS: 1_000,
		OperationMinSample: 20, OperationBudgetBreachRate: 0.10, OperationFailureRate: 0.05,
	}
}

type ProblemReport struct {
	Summary   ProblemSummary
	Problems  []ProblemFinding
	Incidents []ProblemFinding
	Coverage  []CategoryCoverage
	Registry  []DetectorMetadata
}

func BuildProblemReport(summary Summary) (ProblemReport, error) {
	return BuildProblemReportWithConfig(summary, DefaultProblemDetectorConfig())
}

func BuildProblemReportWithConfig(summary Summary, cfg ProblemDetectorConfig) (ProblemReport, error) {
	return buildProblemReportWithCatalog(summary, cfg, nil)
}

func buildProblemReportWithCatalog(
	summary Summary,
	cfg ProblemDetectorConfig,
	catalog *DependencyInjectionCatalog,
) (ProblemReport, error) {
	if err := validateProblemDetectorConfig(cfg); err != nil {
		return ProblemReport{}, err
	}
	b := problemBuilder{summary: summary, cfg: cfg, dependencyInjection: catalog}
	b.detectProcessExit()
	b.detectOperations()
	b.detectStallsAndIO()
	b.detectDatabaseScenarios()
	b.detectDatabasePlanEvidence()
	b.detectDatabaseCalls()
	b.detectDatabaseTransactions()
	b.detectAndroidComponents()
	b.detectUI()
	b.detectSemanticWork()
	b.detectNetwork()
	b.detectMemory()
	b.detectCPU()
	b.detectRuntimeAnalysis()
	b.detectPower()
	b.detectLogSpam()
	b.classifyDependencyInjectionFindings()
	b.finishFindings()
	coverage := b.coverage()
	incidents := buildProblemIncidents(b.findings)
	problemSummary := summarizeProblems(incidents, coverage)
	problemSummary.SignalTotal = len(b.findings)
	problemSummary.Headline = problemSummaryHeadline(problemSummary)
	report := ProblemReport{
		Summary:   problemSummary,
		Problems:  b.findings,
		Incidents: incidents,
		Coverage:  coverage,
		Registry:  detectorRegistry(cfg),
	}
	if err := validateProblemReport(report); err != nil {
		return ProblemReport{}, err
	}
	return report, nil
}

func CompareProblems(baseline, candidate Summary, cohortsComparable bool) ProblemComparison {
	baselineProblems := problemIncidentsOrFindings(baseline)
	candidateProblems := problemIncidentsOrFindings(candidate)
	before := make(map[string]ProblemFinding, len(baselineProblems))
	after := make(map[string]ProblemFinding, len(candidateProblems))
	for _, finding := range baselineProblems {
		before[finding.Fingerprint] = finding
	}
	for _, finding := range candidateProblems {
		after[finding.Fingerprint] = finding
	}
	keys := make([]string, 0, len(before)+len(after))
	seen := map[string]struct{}{}
	for key := range before {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range after {
		if _, ok := seen[key]; !ok {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	deltas := make([]ProblemDelta, 0, len(keys))
	current := make([]ProblemFinding, 0, len(after))
	for _, key := range keys {
		baselineFinding, hasBaseline := before[key]
		candidateFinding, hasCandidate := after[key]
		delta := ProblemDelta{Fingerprint: key, Comparable: cohortsComparable}
		switch {
		case !hasBaseline:
			delta.Status = "new"
			candidateFinding.Status = "new"
		case !hasCandidate:
			delta.Status = "resolved"
			baselineFinding.Status = "resolved"
		case candidateFinding.InvestigationPriority >= baselineFinding.InvestigationPriority+10 || problemSeverityRank(candidateFinding.Severity) > problemSeverityRank(baselineFinding.Severity):
			delta.Status = "regressed"
			candidateFinding.Status = "regressed"
		case candidateFinding.InvestigationPriority <= baselineFinding.InvestigationPriority-10 || problemSeverityRank(candidateFinding.Severity) < problemSeverityRank(baselineFinding.Severity):
			delta.Status = "improved"
			candidateFinding.Status = "improved"
		default:
			delta.Status = "persistent"
			candidateFinding.Status = "persistent"
		}
		if !cohortsComparable {
			delta.Note = "Состав прогонов различается; статус показан как наблюдаемое изменение, но не является доказанной регрессией."
		}
		if hasBaseline {
			copy := baselineFinding
			delta.Baseline = &copy
		}
		if hasCandidate {
			copy := candidateFinding
			delta.Candidate = &copy
			current = append(current, candidateFinding)
		}
		deltas = append(deltas, delta)
	}
	sort.Slice(deltas, func(i, j int) bool {
		left, right := problemDeltaRank(deltas[i].Status), problemDeltaRank(deltas[j].Status)
		if left != right {
			return left > right
		}
		leftPriority, rightPriority := deltaPriority(deltas[i]), deltaPriority(deltas[j])
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return deltas[i].Fingerprint < deltas[j].Fingerprint
	})
	sort.Slice(current, func(i, j int) bool {
		if problemDeltaRank(current[i].Status) != problemDeltaRank(current[j].Status) {
			return problemDeltaRank(current[i].Status) > problemDeltaRank(current[j].Status)
		}
		if current[i].InvestigationPriority != current[j].InvestigationPriority {
			return current[i].InvestigationPriority > current[j].InvestigationPriority
		}
		return current[i].Fingerprint < current[j].Fingerprint
	})
	summary := summarizeProblems(current, candidate.CategoryCoverage)
	return ProblemComparison{SchemaVersion: ProblemSchemaVersion, Summary: summary, Deltas: deltas}
}

func problemDeltaRank(status string) int {
	switch status {
	case "improved":
		return 1
	case "persistent":
		return 2
	case "new":
		return 3
	case "regressed":
		return 4
	default:
		return 0
	}
}
func deltaPriority(delta ProblemDelta) int {
	if delta.Candidate != nil {
		return delta.Candidate.InvestigationPriority
	}
	if delta.Baseline != nil {
		return delta.Baseline.InvestigationPriority
	}
	return 0
}

func validateProblemDetectorConfig(cfg ProblemDetectorConfig) error {
	if cfg.Version == "" || cfg.HTTPSlowMS == 0 || cfg.HTTPHighMS < cfg.HTTPSlowMS ||
		cfg.HTTPMinSample == 0 || cfg.HTTPFailureRate <= 0 || cfg.HTTPFailureRate > 1 ||
		cfg.HTTPStormRate <= 0 || cfg.HTTPStormMinCount == 0 || cfg.UIJankRate <= 0 ||
		cfg.UIHighJankRate < cfg.UIJankRate || cfg.UIMinFrames == 0 || cfg.UIFrameTailMS == 0 ||
		cfg.StallMS == 0 || cfg.StallHighMS < cfg.StallMS || cfg.IOMainThreadMS == 0 ||
		cfg.IOBackgroundMS < cfg.IOMainThreadMS || cfg.IOStormMinCount == 0 || cfg.IOStormRate <= 0 ||
		cfg.IOFailureMinCount == 0 || cfg.IOFailureRate <= 0 || cfg.IOFailureRate > 1 ||
		cfg.IOLargeMainBytes == 0 || cfg.IOLargeBackgroundBytes < cfg.IOLargeMainBytes || cfg.IOSmallOperationBytes == 0 ||
		cfg.DatabaseMainThreadMS == 0 || cfg.DatabaseBackgroundMS < cfg.DatabaseMainThreadMS ||
		cfg.DatabaseStormMinCount == 0 || cfg.DatabaseStormRate == 0 || cfg.DatabaseRapidRepeatCount == 0 ||
		cfg.DatabaseFailureRate <= 0 || cfg.DatabaseFailureRate > 1 ||
		cfg.DatabaseTransactionMainMS == 0 ||
		cfg.DatabaseTransactionBackgroundMS < cfg.DatabaseTransactionMainMS ||
		cfg.DatabaseTransactionStatements == 0 ||
		cfg.LogSpamMinCount == 0 ||
		cfg.LogSpamRate <= 0 || cfg.ProcessCPUPercent <= 0 || cfg.ProcessCPUPercent > 100 ||
		cfg.AsyncQueueWaitMS == 0 || cfg.AsyncQueueMinSamples == 0 || cfg.GCBlockingTimeMS == 0 ||
		cfg.StartupColdResumeMS == 0 || cfg.ScreenResumeMS == 0 || cfg.OperationMinSample == 0 ||
		cfg.OperationBudgetBreachRate <= 0 || cfg.OperationBudgetBreachRate > 1 ||
		cfg.OperationFailureRate <= 0 || cfg.OperationFailureRate > 1 {
		return fmt.Errorf("invalid problem detector config %q", cfg.Version)
	}
	return nil
}

type problemBuilder struct {
	summary             Summary
	cfg                 ProblemDetectorConfig
	dependencyInjection *DependencyInjectionCatalog
	findings            []ProblemFinding
}

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

func (b *problemBuilder) detectNetwork() {
	locationsByRoute := networkProblemLocations(b.summary)
	for _, route := range b.summary.Routes {
		count := uint64(max(route.Count, 0))
		failures := uint64(max(route.Failures, 0))
		rate := ratePerSecond(count, b.summary.DurationMS)
		failureRate := problemRatio(failures, count)
		slow := route.P95MS >= b.cfg.HTTPSlowMS
		failed := count > 0 && failureRate >= b.cfg.HTTPFailureRate
		storm := count >= b.cfg.HTTPStormMinCount && float64(route.PeakRequestsPerSecond) >= b.cfg.HTTPStormRate
		retried := route.Retries > 0 || route.ConnectFailures > 0 || route.TLSFailures > 0
		if !slow && !failed && !storm {
			continue
		}
		impact := 12
		if failed {
			impact += 10
		}
		if storm {
			impact += 6
		}
		impact = min(impact, 32)
		magnitude := 0
		if slow {
			magnitude = min(18, 7+int(route.P95MS/b.cfg.HTTPSlowMS)*4)
		}
		if failed {
			magnitude = max(magnitude, min(25, 8+int(failureRate/b.cfg.HTTPFailureRate)*4))
		}
		exposure := min(20, 4+int(math.Log2(float64(count)+1))*2)
		if storm {
			exposure = max(exposure, 16)
		}
		compound := 0
		symptoms := boolCount(slow, failed, storm, retried)
		if symptoms > 1 {
			compound = min(5, symptoms+1)
		}
		if count < b.cfg.HTTPMinSample && !storm {
			// A single failure is real, but it does not establish a typical failure rate. Keep the
			// route visible as a signal to verify without ranking it as a confirmed recurring defect.
			impact = min(impact, 16)
			magnitude = min(magnitude, 10)
			exposure = min(exposure, 4)
			compound = 0
		}
		where := locationsByRoute[route.Route]
		title := fmt.Sprintf("%s работает медленно", displayUnknown(route.Route, "Неизвестный маршрут"))
		if storm && slow {
			title = fmt.Sprintf("%s создаёт частый и медленный поток запросов", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if storm {
			title = fmt.Sprintf("%s создаёт шторм запросов", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if failed && count == 1 {
			title = fmt.Sprintf("%s завершился ошибкой в единственном наблюдении", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if failed && count < b.cfg.HTTPMinSample {
			title = fmt.Sprintf("%s завершался ошибкой в небольшой выборке", displayUnknown(route.Route, "Неизвестный маршрут"))
		} else if failed {
			title = fmt.Sprintf("%s часто завершается ошибкой", displayUnknown(route.Route, "Неизвестный маршрут"))
		}
		evidence := []ProblemEvidence{{Name: "Число запросов", Observed: fmt.Sprint(count), Unit: "requests", Sample: u64ptr(count), Source: "typed_http"}}
		if slow {
			evidence = append(evidence, ProblemEvidence{Name: upperFiveDurationLabel, Observed: fmt.Sprint(route.P95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.HTTPSlowMS), Sample: u64ptr(count), Source: "typed_http"})
		}
		if failed {
			evidence = append(evidence, ProblemEvidence{Name: "Доля ошибок", Observed: formatPercent(failureRate * 100), Unit: "%", ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.HTTPFailureRate*100), Numerator: u64ptr(failures), Denominator: u64ptr(count), Source: "typed_http"})
		}
		if storm {
			evidence = append(evidence, ProblemEvidence{Name: "Пиковая частота за 1 секунду", Observed: fmt.Sprint(route.PeakRequestsPerSecond), Unit: "requests/s", ExpectedOrThreshold: fmt.Sprintf("< %.2f requests/s", b.cfg.HTTPStormRate), Sample: u64ptr(count), Source: "typed_http_completion_window"})
		}
		if phase, ok := dominantHTTPPhase(route.Phases); ok {
			evidence = append(evidence, ProblemEvidence{Name: "Граница верхних 5% фазы «" + httpPhaseProblemLabel(phase.Name) + "»", Observed: fmt.Sprint(phase.P95MS), Unit: "ms", Sample: u64ptr(uint64(phase.SampleCount)), Source: "typed_http_phase"})
		}
		if route.Retries > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Повторы запросов", Observed: fmt.Sprint(route.Retries), Unit: "attempts", Source: "typed_http_attempts"})
		}
		if route.MaxConcurrency > 0 {
			evidence = append(evidence, ProblemEvidence{Name: "Максимальная одновременность", Observed: fmt.Sprint(route.MaxConcurrency), Unit: "requests", Source: "typed_http_intervals"})
		}
		wall := route.TotalDurationMS
		if wall == 0 {
			wall = uint64(route.Count) * route.P50MS
		}
		bytes := route.BytesRx + route.BytesTx
		confidence, reasons, limits := problemConfidence(b.summary, count, b.cfg.HTTPMinSample, true)
		if storm {
			limits = append(limits, "Пик частоты рассчитан по секундам завершения. Одновременность восстановлена отдельно из интервалов, но миллисекундная точность не позволяет определить порядок событий внутри одной миллисекунды.")
			if route.BurstEstimateStatus == "bounded_approximation" {
				limits = append(limits, "Пиковая частота оценена приближённо из-за длительного или неупорядоченного потока событий.")
			}
		}
		b.add(ProblemFinding{
			DetectorID: "network.route_health", DetectorVersion: b.cfg.Version, Category: ProblemCategoryNetwork, Subcategory: networkSubcategory(slow, failed, storm),
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: networkWhat(route, failures, count, slow, storm, b.cfg.HTTPMinSample), Where: where,
			Why:    ProblemWhy{ClaimLevel: "unknown", Summary: "Проблема маршрута измерена, а фазы и точные места вызова сужают поиск; причинность всё равно нужно подтвердить в указанном коде.", Factors: networkFactors(route, slow, failed, storm)},
			Impact: []string{"Задержка или ошибка пользовательского сценария", "Лишняя сетевая и серверная нагрузка при частых вызовах"}, Evidence: evidence,
			Frequency: &ProblemFrequency{Count: count, RatePerSec: rate}, Cost: &ProblemCost{WallTimeMS: nonZeroU64Ptr(wall), Bytes: nonZeroU64Ptr(bytes)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), compound, "задержка или ошибка для пользователя", "отклонение времени ответа", "частота в прогоне", "число контекстов", "сочетание сетевых симптомов"),
			Recommendations:   []ProblemRecommendation{{Action: "Проверить место вызова, убрать лишние повторы и сократить время ответа маршрута", Rationale: "Исправление уменьшит задержку пользователя, а при повторных запросах — ещё и сетевую нагрузку.", Verification: "Повторить тот же сценарий и сравнить число запросов, ошибки, задержку верхних 5% запросов и суммарное время ожидания."}},
			Limitations:       limits, Drilldowns: []ProblemDrilldown{{Label: "Сеть", Anchor: "network", Filter: route.Route}},
		})
	}
	b.detectWebSockets()
}

func (b *problemBuilder) detectWebSockets() {
	analysis := b.summary.WebSocketAnalysis
	if analysis == nil {
		return
	}
	for _, connection := range analysis.Connections {
		terminals := saturatingUint64Sum(connection.Closed, connection.Failures)
		failureRate := problemRatio(connection.Failures, terminals)
		failureStorm := terminals >= 3 && failureRate >= 0.25
		reconnectStorm := connection.Reconnects >= 3
		slowConnect := connection.Opened >= 3 && connection.ConnectP95MS >= 1_500
		rapidChurn := terminals >= 5 && connection.LifetimeP95MS > 0 && connection.LifetimeP95MS <= 5_000
		if !failureStorm && !reconnectStorm && !slowConnect && !rapidChurn {
			continue
		}
		where := []ProblemLocation{{
			Screen: connection.Screen, Operation: connection.Operation,
			Route: connection.Route, Owner: connection.Owner,
		}}
		evidence := []ProblemEvidence{{
			Name: "Открытия соединения", Observed: fmt.Sprint(connection.Opened),
			Unit: "events", Sample: u64ptr(connection.Opened), Source: "typed_websocket_lifecycle",
		}}
		if failureStorm {
			evidence = append(evidence, ProblemEvidence{
				Name: "Доля обрывов", Observed: formatPercent(failureRate * 100), Unit: "%",
				ExpectedOrThreshold: "< 25%", Numerator: u64ptr(connection.Failures),
				Denominator: u64ptr(terminals), Source: "typed_websocket_lifecycle",
			})
		}
		if reconnectStorm {
			evidence = append(evidence, ProblemEvidence{
				Name: "Переподключения", Observed: fmt.Sprint(connection.Reconnects), Unit: "events",
				ExpectedOrThreshold: "< 3", Source: "typed_websocket_lifecycle",
			})
		}
		if slowConnect {
			evidence = append(evidence, ProblemEvidence{
				Name: "Граница верхних 5% времени подключения", Observed: fmt.Sprint(connection.ConnectP95MS), Unit: "ms",
				ExpectedOrThreshold: "< 1500 ms", Source: "typed_websocket_lifecycle",
			})
		}
		if rapidChurn {
			evidence = append(evidence, ProblemEvidence{
				Name: "Граница верхних 5% времени жизни", Observed: fmt.Sprint(connection.LifetimeP95MS), Unit: "ms",
				ExpectedOrThreshold: "> 5000 ms для устойчивого канала", Source: "typed_websocket_lifecycle",
			})
		}
		confidence, reasons, limits := problemConfidence(b.summary, connection.Opened, 3, true)
		b.add(ProblemFinding{
			DetectorID: "network.websocket_health", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryNetwork, Subcategory: "websocket_instability", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title: fmt.Sprintf("WebSocket %s нестабилен", displayUnknown(connection.Route, "без маршрута")),
			WhatHappened: fmt.Sprintf("Открытий: %d, сбоев: %d, переподключений: %d; 95%% подключений завершились не дольше чем за %d мс.",
				connection.Opened, connection.Failures, connection.Reconnects, connection.ConnectP95MS),
			Where:     where,
			Why:       ProblemWhy{ClaimLevel: "unknown", Summary: "Жизненный цикл показывает нестабильность канала, но причина требует проверки сети, сервера и клиентской политики переподключения."},
			Impact:    []string{"Потеря или задержка данных реального времени", "Лишние подключения, трафик и расход батареи"},
			Evidence:  evidence,
			Frequency: &ProblemFrequency{Count: connection.Opened, RatePerSec: ratePerSecond(connection.Opened, b.summary.DurationMS)},
			Cost:      &ProblemCost{Bytes: nonZeroU64Ptr(connection.ReceivedBytes)},
			PriorityBreakdown: priority(24, 17, min(20, 5+int(connection.Opened)), locationBreadth(where), boolCount(failureStorm, reconnectStorm, slowConnect, rapidChurn),
				"канал реального времени недоступен или запаздывает", "обрывы, повторные подключения или медленное соединение", "число открытий в прогоне", "маршрут и контекст известны", "сочетание симптомов WebSocket"),
			Recommendations: []ProblemRecommendation{{
				Action:       "Проверить причину закрытия и политику переподключения; добавить экспоненциальную задержку со случайным разбросом и ограничением числа попыток",
				Rationale:    "Это предотвращает синхронный шторм повторных подключений и снижает нагрузку при недоступности сети или сервера.",
				Verification: "Повторить сценарий и сравнить число обрывов, переподключений, границу верхних 5% времени подключения и время жизни соединения.",
			}},
			Limitations: limits, Drilldowns: []ProblemDrilldown{{Label: "WebSocket", Anchor: "network", Filter: connection.Route}},
		})
	}
}

func (b *problemBuilder) detectUI() {
	for _, screen := range b.summary.Screens {
		if screen.Frames < b.cfg.UIMinFrames {
			continue
		}
		tailThresholdMS := b.cfg.UIFrameTailMS
		if screen.FrameDeadlineStatus == "consistent" && screen.FrameDeadlineUS > 0 {
			tailThresholdMS = maxUint64(1, (screen.FrameDeadlineUS*2+999)/1_000)
		}
		badRate := screen.JankRatePct >= b.cfg.UIJankRate
		badTail := screen.FrameP95MS >= tailThresholdMS || screen.FrameP99MS >= tailThresholdMS*2
		if !badRate && !badTail {
			continue
		}
		magnitude := min(25, 8+int(screen.JankRatePct/b.cfg.UIJankRate)*4)
		if badTail {
			magnitude = max(magnitude, min(25, 8+int(screen.FrameP95MS/tailThresholdMS)*4))
		}
		exposure := min(20, 6+int(math.Log2(float64(screen.Frames))))
		confidence, reasons, limits := problemConfidence(b.summary, screen.Frames, b.cfg.UIMinFrames, true)
		if screen.FrameDeadlineStatus != "consistent" || screen.FrameDeadlineUS == 0 {
			limits = append(limits, "Бюджет кадра различается между окнами; применён фиксированный порог детектора.")
		}
		if screen.FrameSource != "jankstats" {
			limits = append(limits, "Источник кадров — "+problemFrameSourceLabel(screen.FrameSource)+"; интервал обратных вызовов Choreographer не равен длительности кадра JankStats.")
			confidence = capProblemConfidence(confidence, "medium")
		}
		where := []ProblemLocation{{Screen: screen.Screen}}
		title := fmt.Sprintf("Экран %s заметно дёргается", displayUnknown(screen.Screen, "без атрибуции"))
		what := fmt.Sprintf("Подтормаживали %.1f%% кадров (%d из %d). Задержка верхних 5%% кадров — %d мс, отдельных худших кадров — до %d мс.", screen.JankRatePct, screen.JankyFrames, screen.Frames, screen.FrameP95MS, screen.FrameP99MS)
		why := "Подтормаживания измерены на указанном экране. Связанные работы из того же сценария показаны в разделе «Сценарии и причины»."
		evidence := []ProblemEvidence{{Name: "Доля медленных кадров", Observed: formatPercent(screen.JankRatePct), Unit: "%", ExpectedOrThreshold: fmt.Sprintf("< %.1f%%", b.cfg.UIJankRate), Sample: u64ptr(screen.Frames), Numerator: u64ptr(screen.JankyFrames), Denominator: u64ptr(screen.Frames), Source: "ui_window"}, {Name: "Задержка верхних 5% кадров", Observed: fmt.Sprint(screen.FrameP95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", tailThresholdMS), Sample: u64ptr(screen.Frames), Source: "ui_frame_histogram"}, {Name: "Целевое время кадра", Observed: formatFrameDeadline(screen.FrameDeadlineUS), Unit: "ms", Source: "ui_window_deadline"}}
		var frequency *ProblemFrequency
		if screen.JankyFrames > 0 {
			frequency = &ProblemFrequency{Count: screen.JankyFrames}
		} else if badTail {
			title = fmt.Sprintf("Кадры экрана %s выходят за целевое время", displayUnknown(screen.Screen, "без атрибуции"))
			what = fmt.Sprintf("Системный признак jank не сработал, однако верхние 5%% из %d кадров занимали до %d мс, а отдельные худшие — до %d мс. Поэтому значение 0%% нельзя считать нормой.", screen.Frames, screen.FrameP95MS, screen.FrameP99MS)
			why = "Длинные кадры подтверждены распределением их длительности. Связанные работы из того же сценария показаны в разделе «Сценарии и причины»."
			evidence[0] = ProblemEvidence{Name: "Кадры с системным признаком jank", Observed: "не отмечены", Sample: u64ptr(screen.Frames), Source: "ui_window"}
		}
		b.add(ProblemFinding{
			DetectorID: "ui.jank_tail", DetectorVersion: b.cfg.Version, Category: ProblemCategoryUI, Subcategory: "jank",
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons,
			Title:        title,
			WhatHappened: what,
			Where:        where, Why: ProblemWhy{ClaimLevel: "unknown", Summary: why},
			Impact:    []string{"Видимые рывки и задержка реакции интерфейса"},
			Evidence:  evidence,
			Frequency: frequency, PriorityBreakdown: priority(28, magnitude, exposure, 2, boolScore(badRate && badTail, 4), "деградация UI", "доля медленных и худшие кадры", "размер выборки кадров", "один экран", "доля и задержка кадров согласованы"),
			Recommendations: []ProblemRecommendation{{Action: "Профилировать длинные кадры на этом экране и убрать тяжёлую работу с главного потока", Rationale: "Доля медленных кадров и верхние значения задержки показывают деградацию, которую может скрывать средний FPS.", Verification: "Повторить сценарий минимум на 120 кадрах и сравнить долю медленных кадров и распределение их длительности с бюджетом дисплея."}},
			Limitations:     limits, Drilldowns: []ProblemDrilldown{{Label: "Стабильность и UI", Anchor: "stability-ui", Filter: screen.Screen}},
		})
	}
}

func (b *problemBuilder) detectStallsAndIO() {
	b.detectTypedIO()
	for _, window := range b.summary.ProblemWindows {
		stall := window.Kind == "main_thread_stall" || window.Kind == "main_thread_dispatch"
		if !stall {
			continue
		}
		threshold := b.cfg.StallMS
		if window.MaxMS < threshold {
			continue
		}
		category, subcategory := ProblemCategoryStability, "main_thread_stall"
		title, impact := fmt.Sprintf("Главный поток останавливался до %d мс", window.MaxMS), 32
		magnitude := min(25, 8+int(window.MaxMS/threshold)*4)
		exposure := min(20, 5+int(math.Log2(float64(window.Count)+1))*3)
		confidence, reasons, limits := problemConfidence(b.summary, window.Count, 3, true)
		stack := BestMainThreadStallStack(b.summary.Owners, window.Owner)
		diagnosis := DiagnoseMainThreadStall(window.Owner, stack)
		if stack != "" {
			title = fmt.Sprintf("%s: до %d мс", diagnosis.Title, window.MaxMS)
		}
		where := []ProblemLocation{{Screen: window.Screen, Operation: window.Operation, Owner: window.Owner, Method: stack}}
		claim := "unknown"
		why := "Зафиксирована остановка главного потока; источник внутри интервала не связан строгим идентификатором."
		if stack != "" {
			claim = "correlated"
			why = "Остановка главного потока измерена напрямую; связанный с тем же владельцем снимок стека указывает на " + stack + ". Снимок локализует место выполнения, но не доказывает, что вся длительность паузы потрачена в конечном методе стека."
		}
		b.add(ProblemFinding{
			DetectorID: "stability.main_thread_stall", DetectorVersion: b.cfg.Version,
			Category: category, Subcategory: subcategory, Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
			WhatHappened: fmt.Sprintf("%d событий в %d окнах, максимум %d мс, суммарное окно %d мс.", window.Count, window.Windows, window.MaxMS, window.TotalWindowMS), Where: where,
			Why: ProblemWhy{ClaimLevel: claim, Summary: why}, Impact: []string{"Задержка ввода, пропуски кадров и риск ANR при повторении"},
			Evidence:  []ProblemEvidence{{Name: "Максимальная блокировка", Observed: fmt.Sprint(window.MaxMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", threshold), Sample: u64ptr(window.Count), Source: "problem_window"}, {Name: "Число событий", Observed: fmt.Sprint(window.Count), Unit: "events", Source: "problem_window"}},
			Frequency: &ProblemFrequency{Count: window.Count}, Cost: &ProblemCost{MainThreadBlockedMS: nonZeroU64Ptr(window.TotalWindowMS)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), 0, "блокировка UI", "длительность", "повторяемость", "контекст", "связанный источник отсутствует"),
			Recommendations:   []ProblemRecommendation{{Action: diagnosis.Action, Rationale: diagnosis.Explanation, Verification: "Повторить тот же сценарий и сравнить максимальное и суммарное время блокировки; временную связь с медленными кадрами подтвердить общей трассой."}},
			Limitations:       limits, Drilldowns: []ProblemDrilldown{{Label: "Таймлайн", Anchor: "timeline", Filter: window.Owner}},
		})
	}
}

func (b *problemBuilder) detectTypedIO() {
	if b.summary.IOAnalysis != nil {
		for _, operation := range b.summary.IOAnalysis.Calls {
			b.detectCriticalIO(operation)
		}
	}
}

func (b *problemBuilder) detectDatabaseCalls() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil {
		return
	}
	for _, statement := range analysis.Statements {
		assessment := AssessDatabaseStatement(statement, b.cfg)
		if !assessment.IsProblem() {
			continue
		}
		callEstimate := DatabaseStatementCallEstimate(statement)
		callText := fmt.Sprint(statement.Overall.Calls)
		if callEstimate > statement.Overall.Calls {
			callText = fmt.Sprintf("≈ %d, детально сохранено %d", callEstimate, statement.Overall.Calls)
		}

		linkedToUI := assessment.MainThreadSlow && statement.MainCorrelation.UIWindowOverlaps > 0 &&
			statement.MainCorrelation.UIOverlapMaxDurationUS >= b.cfg.DatabaseMainThreadMS*1_000
		where := databaseProblemLocations(statement.Contexts, linkedToUI)
		owner := where[0].Owner
		p50MS := microsecondsToMillisecondsCeil(statement.Overall.P50DurationUS)
		p95MS := microsecondsToMillisecondsCeil(statement.Overall.P95DurationUS)
		maxMS := microsecondsToMillisecondsCeil(statement.Overall.MaxDurationUS)
		totalMS := microsecondsToMillisecondsCeil(statement.Overall.TotalDurationUS)
		confidence, reasons, limits := problemConfidence(b.summary, statement.Overall.Calls, 5, true)
		limits = append(limits,
			"SQL приводится к безопасному шаблону без значений параметров; динамически собранный текст может быть недоступен.",
			"Быстрые повторы указывают на возможное дублирование или N+1, но не доказывают его без связи с вызывающим сценарием.",
			"По журналу вызовов нельзя надёжно определить неиспользуемые строки и таблицы: для этого нужны снимки схемы, размеров и обращений за длительный период.",
		)
		if statement.Telemetry.PhaseMeasuredCalls == 0 {
			limits = append(limits, "Общую длительность нельзя разделить на ожидание соединения, блокировку, выполнение и чтение результата без измерений этих фаз адаптером приложения.")
		}
		if statement.Overall.QuantilesApproximated || statement.Main.QuantilesApproximated ||
			statement.Background.QuantilesApproximated {
			limits = append(
				limits,
				"После 128 выполнений квантили рассчитываются потоковым детерминированным алгоритмом с ограниченной памятью; максимум и количество остаются точными.",
			)
		}

		signals := make([]string, 0, 5)
		if assessment.MainThreadSlow {
			signals = append(signals, "SQL-вызов занимает бюджет кадра на главном потоке")
		}
		if assessment.BackgroundSlow {
			signals = append(signals, "SQL-вызов медленно выполняется в фоне")
		}
		if assessment.Storm {
			signals = append(signals, "зафиксирован всплеск частоты")
		}
		if assessment.Repeated {
			signals = append(signals, "SQL-вызов быстро повторяется")
		}
		if assessment.Failures {
			failureKinds := databaseFailureKindSummary(statement.Telemetry.FailureKinds)
			if failureKinds == "" {
				signals = append(signals, "есть неуспешные выполнения")
			} else {
				signals = append(signals, "есть неуспешные выполнения: "+failureKinds)
			}
		}

		evidence := []ProblemEvidence{
			{Name: "Число вызовов БД", Observed: callText, Unit: "events", Sample: u64ptr(statement.Overall.Calls), Source: "typed_database"},
			{Name: medianDurationLabel, Observed: fmt.Sprint(p50MS), Unit: "ms", Source: "typed_database"},
			{Name: upperFiveDurationLabel, Observed: fmt.Sprint(p95MS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms в фоне; < %d ms на главном потоке", b.cfg.DatabaseBackgroundMS, b.cfg.DatabaseMainThreadMS), Source: "typed_database"},
			{Name: "Максимальная длительность", Observed: fmt.Sprint(maxMS), Unit: "ms", Source: "typed_database"},
			{Name: "На главном потоке", Observed: fmt.Sprint(statement.Main.Calls), Unit: "events", Denominator: u64ptr(statement.Overall.Calls), Source: "typed_database"},
			{Name: "Пиковая частота", Observed: fmt.Sprint(statement.PeakCallsPerSecond), Unit: "events/s", ExpectedOrThreshold: fmt.Sprintf("< %d events/s", b.cfg.DatabaseStormRate), Source: "typed_database"},
			{Name: "Быстрые повторы", Observed: fmt.Sprint(statement.RapidRepeats), Unit: "events", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.DatabaseRapidRepeatCount), Source: "typed_database"},
			{Name: "Ошибки", Observed: fmt.Sprint(statement.Overall.Failures), Unit: "events", Denominator: u64ptr(statement.Overall.Calls), Source: "typed_database"},
		}
		if callEstimate > statement.Overall.Calls {
			evidence = append(evidence, ProblemEvidence{
				Name: "Оценка погрешности частоты", Observed: fmt.Sprint(statement.FrequencyEstimateError),
				Unit: "events", Source: "typed_database_count_min_sketch",
			})
		}
		if failureKinds := databaseFailureKindSummary(statement.Telemetry.FailureKinds); failureKinds != "" {
			evidence = append(evidence, ProblemEvidence{
				Name: "Классы ошибок", Observed: failureKinds, Sample: u64ptr(statement.Overall.Failures),
				Source: "typed_database_failure_taxonomy",
			})
		}
		if statement.Telemetry.ResultKnownCalls > 0 {
			evidence = append(evidence, ProblemEvidence{
				Name: "Вызовы с известным результатом", Observed: fmt.Sprintf("%d / %d", statement.Telemetry.ResultKnownCalls, statement.Overall.Calls),
				Sample: u64ptr(statement.Telemetry.ResultKnownCalls), Denominator: u64ptr(statement.Overall.Calls),
				Source: "typed_database_result",
			})
		}
		evidence = append(evidence, databasePhaseEvidence(statement.Telemetry)...)
		if assessment.MainThreadSlow {
			evidence = append(evidence, databaseThreadDurationEvidence(
				"На главном потоке", statement.Main, assessment.MainDurationUS,
				assessment.MainUsesMaximum, b.cfg.DatabaseMainThreadMS,
			))
		}
		if assessment.BackgroundSlow {
			evidence = append(evidence, databaseThreadDurationEvidence(
				"В фоне", statement.Background, assessment.BackgroundDurationUS,
				assessment.BackgroundUsesMaximum, b.cfg.DatabaseBackgroundMS,
			))
		}
		if linkedToUI {
			evidence = append(evidence, ProblemEvidence{
				Name:     "Пересечения SQL на главном потоке с проблемным окном UI",
				Observed: fmt.Sprint(statement.MainCorrelation.UIWindowOverlaps), Unit: "events",
				Numerator:   u64ptr(statement.MainCorrelation.UIJankyFrames),
				Denominator: u64ptr(statement.MainCorrelation.UIFrames), Source: "typed_database_ui_interval_join",
			})
		}
		if databaseHasRelatedCorrelation(statement.MainCorrelation) ||
			databaseHasRelatedCorrelation(statement.BackgroundCorrelation) {
			evidence = append(evidence, ProblemEvidence{
				Name: "Подсистемы, работавшие в тот же интервал",
				Observed: databaseRelatedThreadCorrelationSummary(
					statement.MainCorrelation,
					statement.BackgroundCorrelation,
				),
				Source: "typed_database_related_interval_join",
			})
			limits = append(limits, "Совпадения с HTTP, фоновыми задачами, файловыми операциями и GC показывают только одновременную работу в том же процессе и сценарии, но не доказывают причину задержки.")
		}
		impact := 18
		if assessment.MainThreadSlow {
			impact = 34
		}
		magnitude := 6
		if assessment.MainThreadSlow {
			mainMS := microsecondsToMillisecondsCeil(assessment.MainDurationUS)
			magnitude = max(magnitude, min(25, 8+int(mainMS/maxUint64(b.cfg.DatabaseMainThreadMS, 1))*4))
		}
		if assessment.BackgroundSlow {
			backgroundMS := microsecondsToMillisecondsCeil(assessment.BackgroundDurationUS)
			magnitude = max(magnitude, min(25, 8+int(backgroundMS/maxUint64(b.cfg.DatabaseBackgroundMS, 1))*4))
		}
		if assessment.Failures {
			magnitude = max(magnitude, min(25, 8+int(assessment.FailureRate/b.cfg.DatabaseFailureRate)*4))
		}
		exposure := min(20, 4+int(math.Log2(float64(callEstimate)+1))*3)
		if assessment.Storm {
			exposure = max(exposure, min(20, 8+int(statement.PeakCallsPerSecond/b.cfg.DatabaseStormRate)*3))
		}

		why := "Событие БД напрямую связывает шаблон SQL, место вызова, поток, длительность и результат выполнения."
		if linkedToUI {
			why = "Выполнение SQL на главном потоке пересеклось с проблемным окном UI в том же процессе, запуске приложения, операции и экране. SQL занимал главный поток во время наблюдаемого ухудшения."
		}
		b.add(ProblemFinding{
			DetectorID: "io.database_calls", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: databaseFindingSubcategory(assessment, linkedToUI), Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:             fmt.Sprintf("Проблемный SQL-вызов в %s", displayUnknown(owner, "неизвестном месте")),
			WhatHappened:      fmt.Sprintf("%s: %s вызовов, граница верхних 5%% длительностей %d мс, максимум %d мс, %d вызовов на главном потоке, пик %d/с. %s.", displayUnknown(statement.Query, "SQL-текст не определён"), callText, p95MS, maxMS, statement.Main.Calls, statement.PeakCallsPerSecond, strings.Join(signals, "; ")),
			Where:             where,
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: why},
			Impact:            []string{"Задержка интерфейса, лишняя нагрузка на хранилище и рост времени пользовательского сценария"},
			Evidence:          evidence,
			Frequency:         &ProblemFrequency{Count: callEstimate, RatePerSec: ratePerSecond(callEstimate, b.summary.DurationMS)},
			Cost:              &ProblemCost{WallTimeMS: nonZeroU64Ptr(totalMS)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), boolScore(databaseAssessmentSignalCount(assessment) > 1, 5), "влияние БД на интерфейс и сценарий", "длительность и ошибки", "частота и повторы", "место SQL-вызова", "несколько независимых признаков"),
			Recommendations:   databaseRecommendations(statement, assessment),
			Limitations:       limits,
			Drilldowns:        []ProblemDrilldown{{Label: "База данных", Anchor: "database-analysis", Filter: firstKnown(statement.Query, owner)}},
		})
	}
}

func (b *problemBuilder) detectDatabasePlanEvidence() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil || analysis.Evidence == nil {
		return
	}
	for _, plan := range analysis.Evidence.Findings {
		where := []ProblemLocation{{Owner: plan.Table, Operation: plan.Operation}}
		if plan.Table == "" {
			where[0].Owner = "план SQL-запроса"
		}
		b.add(ProblemFinding{
			DetectorID: "io.database_plan_evidence", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: "database_plan_" + plan.Kind,
			Status: "observed", Confidence: "medium",
			ConfidenceReasons: []string{
				"шаг плана SQL-запроса импортирован как заранее очищенный структурированный артефакт",
				"отпечаток и операция совпали с наблюдаемым SQL-шаблоном",
			},
			Title:        databasePlanFindingTitle(plan),
			WhatHappened: plan.Summary,
			Where:        where,
			Why: ProblemWhy{
				ClaimLevel: "linked",
				Summary:    "Вывод основан на явно импортированном шаге плана SQL-запроса, а не на оценке задержки по журналу.",
			},
			Impact: []string{"Лишняя работа SQLite возможна, если доступ или временная структура обрабатывают большой объём данных"},
			Evidence: []ProblemEvidence{{
				Name: "Шаг плана SQL-запроса", Observed: databasePlanFindingEvidence(plan),
				Source: "developer_database_evidence",
			}},
			Frequency: &ProblemFrequency{Count: 1},
			PriorityBreakdown: priority(
				18, 12, 6, locationBreadth(where), 2,
				"потенциальная стоимость доступа", "тип шага плана", "один импортированный план",
				"таблица и операция", "точное совпадение отпечатка",
			),
			Recommendations: []ProblemRecommendation{{
				Action:       plan.Action,
				Rationale:    "SCAN или временное B-дерево наблюдаемы в приложенном плане, но сами по себе не доказывают, что новый индекс будет быстрее с учётом селективности и стоимости записи.",
				Verification: "Повторно экспортируйте план и выполните тот же сценарий; сравните шаги плана, задержку БД и корректность результата на репрезентативных данных.",
			}},
			Limitations: []string{
				"Артефакт предоставлен разработчиком отдельно; анализатор проверяет формат и соответствие отпечатка, но не актуальность схемы или набора данных.",
				"Наличие SCAN не означает автоматически отсутствующий индекс или плохой план.",
			},
			Drilldowns: []ProblemDrilldown{{Label: "База данных", Anchor: "database-analysis", Filter: plan.Query}},
		})
	}
}

func databasePlanFindingTitle(plan DatabasePlanFinding) string {
	switch plan.Kind {
	case "scan":
		return "План SQL-запроса содержит SCAN"
	case "temp_btree":
		return "План SQL-запроса использует временное B-дерево"
	case "automatic_index":
		return "План SQL-запроса использует автоматически созданный индекс"
	default:
		return "План SQL-запроса требует проверки"
	}
}

func databasePlanFindingEvidence(plan DatabasePlanFinding) string {
	parts := []string{plan.Kind}
	if plan.Table != "" {
		parts = append(parts, "table="+plan.Table)
	}
	if plan.Index != "" {
		parts = append(parts, "index="+plan.Index)
	}
	if plan.Purpose != "" {
		parts = append(parts, "purpose="+plan.Purpose)
	}
	return strings.Join(parts, ", ")
}

func (b *problemBuilder) detectDatabaseScenarios() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil {
		return
	}
	for _, scenario := range analysis.Scenarios.Candidates {
		where := []ProblemLocation{{
			Screen: scenario.Screen, Operation: scenario.ContextOperation,
			Owner: scenario.Source, Method: scenario.Source,
		}}
		confidence, reasons, limits := problemConfidence(
			b.summary, scenario.AffectedScopes, 2, true,
		)
		limits = append(limits,
			"Нормализованный SQL-шаблон не содержит значений параметров: он подтверждает повтор формы запроса, но не отличает N+1 от полного дублирования.",
			"Пакетная обработка, объединение запросов и кэширование — варианты для проверки, а не автоматический рецепт без знания семантики данных.",
			fmt.Sprintf(
				"Число вызовов рассчитано приближённо; подробно сохранено %d замеров, максимальная погрешность оценки частоты — %s.",
				scenario.RetainedCalls,
				russianCountUint64(analysis.Scenarios.FrequencyEstimateError, "вызов", "вызова", "вызовов"),
			),
		)
		if scenario.CostLowerBound {
			limits = append(limits, "Часть ранних замеров не сохранена; число вызовов оценено приближённо, а показанное время является нижней границей.")
		}
		if analysis.Scenarios.ScopeCountsApproximated {
			limits = append(limits, "После превышения точного предела число границ сценария рассчитано приближённо.")
		}
		title := "Повторные SQL-вызовы внутри одного сценария"
		if scenario.Kind == "batch_candidate" {
			title = "Кандидат на пакетную запись БД"
		}
		claim := scenario.ClaimLevel
		if claim == "" {
			claim = "hypothesis"
		}
		impact := 18
		if scenario.MainThreadCalls > 0 {
			impact = 30
		}
		magnitude := min(25, 5+int(math.Log2(float64(scenario.MaxCallsPerScope)+1))*4)
		exposure := min(20, 4+int(math.Log2(float64(scenario.AffectedScopes)+1))*4)
		evidence := []ProblemEvidence{
			{Name: "Примерное число повторных вызовов", Observed: fmt.Sprint(scenario.EstimatedCalls), Unit: "calls", Sample: u64ptr(scenario.RetainedCalls), Source: "typed_database_scenario_count_min_sketch"},
			{Name: "Максимум вызовов в одной границе сценария", Observed: fmt.Sprint(scenario.MaxCallsPerScope), Unit: "calls", ExpectedOrThreshold: fmt.Sprintf("< %d", databaseScenarioRepeatCalls), Source: "typed_database_scenario"},
			{Name: "Границы сценария с повторами / все наблюдаемые", Observed: fmt.Sprintf("%d / %d", scenario.AffectedScopes, scenario.ObservedScopes), Numerator: u64ptr(scenario.AffectedScopes), Denominator: u64ptr(scenario.ObservedScopes), Source: "typed_database_scenario"},
			{Name: "Вызовов на одну наблюдаемую границу", Observed: fmt.Sprintf("%.2f", scenario.CallsPerObservedScope), Unit: "calls/boundary", Source: "typed_database_scenario"},
			{Name: "Суммарное время", Observed: fmt.Sprint(microsecondsToMillisecondsCeil(scenario.TotalDurationUS)), Unit: "ms", Source: "typed_database_scenario"},
			{Name: "На главном потоке", Observed: fmt.Sprint(scenario.MainThreadCalls), Unit: "events", Denominator: u64ptr(scenario.EstimatedCalls), Source: "typed_database_scenario"},
		}
		whatHappened, factors := databaseScenarioExplanation(scenario)
		b.add(ProblemFinding{
			DetectorID: "io.database_repeated_in_scope", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: scenario.Kind, Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        title,
			WhatHappened: whatHappened,
			Where:        where,
			Why: ProblemWhy{
				ClaimLevel: claim,
				Summary:    "Один нормализованный SQL-шаблон много раз вызван внутри одной границы сценария. Это подтверждает серию повторов, но без значений параметров нельзя отличить N+1 от допустимых повторов.",
				Factors:    factors,
			},
			Impact:    []string{"Дополнительные обращения к SQLite увеличивают время сценария; вызовы на главном потоке могут задерживать интерфейс"},
			Evidence:  evidence,
			Frequency: &ProblemFrequency{Count: scenario.EstimatedCalls},
			Cost:      &ProblemCost{WallTimeMS: nonZeroU64Ptr(microsecondsToMillisecondsCeil(scenario.TotalDurationUS))},
			PriorityBreakdown: priority(
				impact, magnitude, exposure, locationBreadth(where), boolScore(scenario.Failures > 0, 5),
				"стоимость повторов в сценарии", "число повторов в одной границе", "доля затронутых границ сценария",
				"источник и логическая операция", "ошибки среди повторов",
			),
			Recommendations: []ProblemRecommendation{{
				Action:       "Откройте указанное место вызова и проверьте, можно ли заменить повторы пакетной записью, одним SQL-запросом или кэшем.",
				Rationale:    "Форма SQL-вызова и его суммарное время известны, но значения параметров намеренно не записываются.",
				Verification: "Повторите тот же сценарий: результат должен сохраниться, а максимум вызовов в одной границе и суммарное время БД — уменьшиться.",
			}},
			Limitations: limits,
			Drilldowns: []ProblemDrilldown{{
				Label: "База данных", Anchor: "database-analysis",
				Filter: firstKnown(scenario.Query, scenario.Source),
			}},
		})
	}
}

func databaseScenarioExplanation(scenario DatabaseScenarioStats) (string, []string) {
	boundary := "одной операции приложения"
	affected := fmt.Sprintf("%d операциях приложения", scenario.AffectedScopes)
	if scenario.ScopeKind == "transaction" {
		boundary = "одной транзакции"
		affected = fmt.Sprintf("%d транзакциях", scenario.AffectedScopes)
	}
	location := displayUnknown(scenario.Source, "месте вызова, которое не попало в журнал")
	query := strings.TrimSpace(scenario.Query)
	identity := fmt.Sprintf("Вызов БД из %s", location)
	factors := make([]string, 0, 1)
	if !datavalue.IsUnknown(query) {
		identity = fmt.Sprintf("Вызов БД с SQL-шаблоном «%s»", query)
	} else {
		factors = append(factors, fmt.Sprintf(
			"SQL-шаблон не записан. Проблема локализована по месту вызова %s; откройте этот DAO или метод и проверьте выполняемый им SQL.",
			location,
		))
	}
	return fmt.Sprintf(
		"%s повторялся до %d раз в %s; примерно %d вызовов в %s из %d наблюдаемых, суммарно не менее %d мс.",
		identity,
		scenario.MaxCallsPerScope,
		boundary,
		scenario.EstimatedCalls,
		affected,
		scenario.ObservedScopes,
		microsecondsToMillisecondsCeil(scenario.TotalDurationUS),
	), factors
}

func (b *problemBuilder) detectDatabaseTransactions() {
	analysis := b.summary.DatabaseAnalysis
	if analysis == nil || analysis.Transactions == nil {
		return
	}
	for _, transaction := range analysis.Transactions.Transactions {
		assessment := AssessDatabaseTransaction(transaction, b.cfg)
		if !assessment.IsProblem() {
			continue
		}
		where := []ProblemLocation{{
			Process: transaction.Process, Screen: transaction.Screen,
			Operation: transaction.ContextOperation,
			Owner:     transaction.Source, Method: transaction.Source,
		}}
		confidence, reasons, limits := problemConfidence(b.summary, 1, 1, true)
		limits = append(limits,
			"Общая длительность описывает всю транзакцию; ожидание соединения, блокировку, выполнение и чтение результата нельзя разделить без измерений этих фаз интеграционным адаптером.",
		)
		outcome := databaseTransactionOutcomeExplanation(transaction)
		duration := "длительность не вычисляется: событие завершения отсутствует"
		if transaction.Complete {
			duration = fmt.Sprintf("длительность %d мс", microsecondsToMillisecondsCeil(transaction.DurationUS))
		}
		signals := databaseTransactionSignals(assessment)
		evidence := []ProblemEvidence{
			{Name: "Число SQL-вызовов", Observed: fmt.Sprint(transaction.StatementCount), Unit: "events", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.DatabaseTransactionStatements), Source: "typed_database_transaction"},
			{Name: "Чтения / записи", Observed: fmt.Sprintf("%d / %d", transaction.ReadCount, transaction.WriteCount), Unit: "events", Source: "typed_database_transaction"},
		}
		if transaction.Complete {
			evidence = append(evidence, ProblemEvidence{
				Name: "Длительность транзакции", Observed: fmt.Sprint(microsecondsToMillisecondsCeil(transaction.DurationUS)),
				Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d мс на главном потоке; < %d мс в фоне", b.cfg.DatabaseTransactionMainMS, b.cfg.DatabaseTransactionBackgroundMS),
				Source: "typed_database_transaction",
			})
		} else {
			evidence = append(evidence, ProblemEvidence{
				Name: "Полнота жизненного цикла", Observed: "начало без завершения", Source: "typed_database_transaction",
			})
		}
		if databaseHasRelatedCorrelation(transaction.Correlation) {
			evidence = append(evidence, ProblemEvidence{
				Name:     "Связанные подсистемы в границе транзакции",
				Observed: databaseRelatedCorrelationSummary(transaction.Correlation),
				Source:   "typed_database_transaction_related_interval_join",
			})
			limits = append(limits, "Пересечения с HTTP, фоновыми задачами, файловыми операциями и GC показывают только совпадение по времени и не доказывают конкуренцию за блокировку или причинность.")
		}
		impact := 20
		if assessment.MainThreadSlow || assessment.Failure {
			impact = 34
		} else if assessment.Rollback || assessment.Incomplete {
			impact = 26
		}
		magnitude := 8
		if assessment.MainThreadSlow {
			magnitude = min(25, 10+int(transaction.DurationUS/1_000/b.cfg.DatabaseTransactionMainMS)*3)
		} else if assessment.BackgroundSlow {
			magnitude = min(25, 8+int(transaction.DurationUS/1_000/b.cfg.DatabaseTransactionBackgroundMS)*3)
		}
		if assessment.ManyStatements {
			magnitude = max(magnitude, min(25, 8+int(transaction.StatementCount/b.cfg.DatabaseTransactionStatements)*3))
		}
		b.add(ProblemFinding{
			DetectorID: "io.database_transaction", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryIO, Subcategory: databaseTransactionSubcategory(assessment),
			Status: "observed", Confidence: confidence, ConfidenceReasons: reasons,
			Title: fmt.Sprintf("Проблемная транзакция БД в %s", displayUnknown(transaction.Source, "неизвестном месте")),
			WhatHappened: fmt.Sprintf(
				"Транзакция #%d: %s, %s, %d SQL-вызовов (чтение %d / запись %d); %s.",
				transaction.TransactionID, outcome, duration, transaction.StatementCount,
				transaction.ReadCount, transaction.WriteCount, strings.Join(signals, "; "),
			),
			Where: where,
			Why: ProblemWhy{
				ClaimLevel: "linked",
				Summary:    "События жизненного цикла транзакции напрямую связывают результат, длительность и число SQL-вызовов с этим местом в коде и контекстом. Без измерений отдельных фаз нельзя объяснить, из чего сложилась общая длительность.",
			},
			Impact:   []string{"Долгая или незавершённая атомарная работа увеличивает задержку сценария и дольше удерживает ресурсы БД"},
			Evidence: evidence, Frequency: &ProblemFrequency{Count: 1},
			Cost: &ProblemCost{WallTimeMS: transactionWallTimeMS(transaction)},
			PriorityBreakdown: priority(
				impact, magnitude, 8, locationBreadth(where),
				boolScore(databaseTransactionSignalCount(assessment) > 1, 5),
				"влияние транзакции на сценарий", "длительность, результат и число SQL-вызовов", "наблюдаемый экземпляр",
				"место в коде и контекст", "несколько независимых признаков",
			),
			Recommendations: databaseTransactionRecommendations(assessment, transaction),
			Limitations:     limits,
			Drilldowns:      []ProblemDrilldown{{Label: "Транзакции БД", Anchor: "database-analysis", Filter: transaction.Source}},
		})
	}
}

func databaseTransactionOutcomeExplanation(transaction DatabaseTransactionStats) string {
	switch transaction.Outcome {
	case "failure":
		return "ошибка (" + databaseFailureDisplayName(transaction.FailureKind) + ")"
	case "rollback":
		return "откат"
	case "success":
		return "успешное завершение"
	case "incomplete":
		return "начало без завершения"
	default:
		return transaction.Outcome
	}
}

func databaseTransactionSignals(assessment DatabaseTransactionAssessment) []string {
	result := make([]string, 0, 7)
	if assessment.MainThreadSlow {
		result = append(result, "долгая работа на главном потоке")
	}
	if assessment.BackgroundSlow {
		result = append(result, "долгая фоновая работа")
	}
	if assessment.Failure {
		result = append(result, "неуспешное завершение")
	}
	if assessment.Rollback {
		result = append(result, "явный откат")
	}
	if assessment.ManyStatements {
		result = append(result, "много SQL-вызовов")
	}
	if assessment.Incomplete {
		result = append(result, "жизненный цикл не завершён")
	}
	if assessment.Nested {
		result = append(result, "есть родительская транзакция")
	}
	return result
}

func databaseTransactionSubcategory(assessment DatabaseTransactionAssessment) string {
	switch {
	case assessment.Failure:
		return "database_transaction_failure"
	case assessment.Incomplete:
		return "database_transaction_incomplete"
	case assessment.Rollback:
		return "database_transaction_rollback"
	case assessment.MainThreadSlow:
		return "database_transaction_main_thread"
	case assessment.ManyStatements:
		return "database_transaction_many_statements"
	case assessment.BackgroundSlow:
		return "database_transaction_long"
	default:
		return "database_transaction_nested"
	}
}

func databaseTransactionSignalCount(assessment DatabaseTransactionAssessment) int {
	return boolCount(
		assessment.MainThreadSlow, assessment.BackgroundSlow, assessment.Failure,
		assessment.Rollback, assessment.ManyStatements, assessment.Incomplete, assessment.Nested,
	)
}

func databaseTransactionRecommendations(
	assessment DatabaseTransactionAssessment,
	transaction DatabaseTransactionStats,
) []ProblemRecommendation {
	result := make([]ProblemRecommendation, 0, 4)
	if assessment.MainThreadSlow {
		result = append(result, ProblemRecommendation{
			Action:       "Перенести транзакцию с главного потока и сократить её синхронную границу",
			Rationale:    "Событие завершения подтверждает превышение бюджета кадра именно на главном потоке.",
			Verification: "Повторить операцию: долгих транзакций на главном потоке не должно остаться; длительность и самые медленные кадры UI оцениваются отдельно.",
		})
	}
	if assessment.Failure || assessment.Rollback {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить ветку завершения и правила повторов или отката в указанном месте кода",
			Rationale:    "Жизненный цикл точно фиксирует результат и безопасный класс ошибки, но не хранит текст ошибки и значения параметров SQL.",
			Verification: "Повторить те же входные условия и подтвердить успешное завершение без роста повторов.",
		})
	}
	if assessment.ManyStatements {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить повторные вызовы внутри транзакции и возможность пакетной записи, вставки с обновлением или объединённого запроса",
			Rationale:    fmt.Sprintf("В одной транзакции выполнено %d SQL-вызовов; это наблюдаемый объём, но не автоматический диагноз N+1.", transaction.StatementCount),
			Verification: "В том же сценарии уменьшились число SQL-вызовов, операций чтения и записи и длительность при неизменном результате.",
		})
	}
	if assessment.Incomplete {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить все пути выхода и запись события завершения",
			Rationale:    "Начало зафиксировано, но длительность и результат нельзя восстановить без события завершения.",
			Verification: "В повторном прогоне у каждой начатой транзакции есть событие завершения.",
		})
	}
	if assessment.Nested {
		result = append(result, ProblemRecommendation{
			Action:       "Проверить необходимость родительской и дочерней транзакций и фактическую семантику вложенности адаптера",
			Rationale:    "Идентификатор родителя подтверждает вложенный жизненный цикл, но сам по себе не доказывает ошибку.",
			Verification: "Сопоставить число и результаты родительских и дочерних транзакций; после упрощения вложенность исчезает без нарушения атомарности.",
		})
	}
	if len(result) == 0 && assessment.BackgroundSlow {
		result = append(result, ProblemRecommendation{
			Action:       "Сократить объём работы внутри транзакции и измерить её фазы через доверенный адаптер",
			Rationale:    "Общая длительность превышает фоновый порог, но без фаз нельзя выбрать причину задержки.",
			Verification: "Повторить сценарий и сравнить длительность и число SQL-вызовов; выводы по фазам делать только при наличии их измерений.",
		})
	}
	return result
}

func transactionWallTimeMS(transaction DatabaseTransactionStats) *uint64 {
	if !transaction.Complete {
		return nil
	}
	return nonZeroU64Ptr(microsecondsToMillisecondsCeil(transaction.DurationUS))
}

func databaseProblemLocations(contexts []DatabaseStatementContextStats, preferLinkedUI bool) []ProblemLocation {
	if len(contexts) == 0 {
		return []ProblemLocation{{Owner: "unknown", Method: "unknown"}}
	}
	locations := make([]ProblemLocation, 0, min(len(contexts), databaseProblemLocationLimit))
	for pass := 0; pass < 2 && len(locations) < databaseProblemLocationLimit; pass++ {
		for _, context := range contexts {
			linked := context.MainCorrelation.UIWindowOverlaps > 0
			if preferLinkedUI && linked != (pass == 0) || !preferLinkedUI && pass > 0 {
				continue
			}
			locations = appendUniqueLocation(locations, ProblemLocation{
				Process: context.Process, Screen: context.Screen, Operation: context.ContextOperation,
				Owner: firstKnown(context.Source, context.ContextOwner), Method: context.Source,
			})
			if len(locations) == databaseProblemLocationLimit {
				break
			}
		}
	}
	return locations
}

const databaseProblemLocationLimit = 8

func databaseThreadDurationEvidence(
	thread string,
	stats DatabaseExecutionStats,
	durationUS uint64,
	usesMaximum bool,
	thresholdMS uint64,
) ProblemEvidence {
	metric := "верхние 5%"
	if usesMaximum {
		metric = "максимум (малая выборка)"
	}
	return ProblemEvidence{
		Name:     thread + ": " + metric,
		Observed: fmt.Sprint(microsecondsToMillisecondsCeil(durationUS)), Unit: "ms",
		ExpectedOrThreshold: fmt.Sprintf("< %d ms", thresholdMS), Sample: u64ptr(stats.Calls),
		Source: "typed_database",
	}
}

func databaseFindingSubcategory(assessment DatabaseStatementAssessment, linkedToUI bool) string {
	switch {
	case assessment.MainThreadSlow && linkedToUI:
		return "database_main_thread_ui_linked"
	case assessment.MainThreadSlow:
		return "database_main_thread_slow"
	case assessment.BackgroundSlow:
		return "database_background_slow"
	case assessment.Failures:
		return "database_failures"
	case assessment.Storm:
		return "database_storm"
	case assessment.Repeated:
		return "database_repeated"
	default:
		return "database_call"
	}
}

func databaseAssessmentSignalCount(assessment DatabaseStatementAssessment) int {
	return boolCount(
		assessment.MainThreadSlow,
		assessment.BackgroundSlow,
		assessment.Storm,
		assessment.Repeated,
		assessment.Failures,
	)
}

func databaseRecommendations(
	statement DatabaseStatementStats,
	assessment DatabaseStatementAssessment,
) []ProblemRecommendation {
	recommendations := make([]ProblemRecommendation, 0, 3)
	if assessment.MainThreadSlow {
		recommendations = append(recommendations, ProblemRecommendation{
			Action:       "Перенести вызов БД с главного потока и повторить тот же сценарий",
			Rationale:    "Thread-specific измерение подтверждает превышение бюджета кадра именно на главном потоке.",
			Verification: "На главном потоке остаётся 0 медленных вызовов; фоновые вызовы оцениваются отдельно.",
		})
	}
	if assessment.BackgroundSlow {
		if databaseOperationIsRead(statement.Operation) {
			recommendations = append(recommendations, ProblemRecommendation{
				Action:       "Снять EXPLAIN QUERY PLAN для этого шаблона на репрезентативной схеме",
				Rationale:    "Фоновое чтение превышает порог, но журнал без плана не различает полное сканирование, блокировку и стоимость чтения результата.",
				Verification: "После адресного изменения повторить сценарий и сравнить границу верхних 5% фоновых длительностей при том же числе вызовов.",
			})
		} else {
			recommendations = append(recommendations, ProblemRecommendation{
				Action:       "Проверить размер пакета, границы транзакции и конкуренцию записей в этом сценарии",
				Rationale:    "Фоновая запись превышает порог; план SQL-запроса и объём выборки не объясняют стоимость записи без дополнительных данных.",
				Verification: "Повторить тот же сценарий и сравнить верхние 5% и максимум фоновых длительностей, число вызовов на транзакцию и измеренные фазы ожидания.",
			})
		}
	}
	if assessment.Storm || assessment.Repeated {
		recommendations = append(recommendations, ProblemRecommendation{
			Action:       "Проверить вызывающий сценарий на дублирование, N+1 и возможность пакетной обработки или кэширования",
			Rationale:    "Наблюдается высокая частота или серия близких одинаковых шаблонов; это гипотеза, а не доказанный N+1.",
			Verification: "Сравнить число вызовов, быстрых повторов и пик/с на той же операции.",
		})
	}
	if assessment.Failures {
		failureKinds := databaseFailureKindSummary(statement.Telemetry.FailureKinds)
		rationale := "События БД подтверждают ошибки; текст ошибки, стек и значения параметров SQL намеренно не сохраняются."
		if failureKinds != "" {
			rationale = "События БД подтверждают безопасные классы ошибок: " + failureKinds + ". Текст ошибки, стек и значения параметров SQL не сохраняются."
		}
		recommendations = append(recommendations, ProblemRecommendation{
			Action:       "Локализовать неуспешное завершение в указанном месте кода и воспроизвести входные условия",
			Rationale:    rationale,
			Verification: "Повторный прогон не содержит ошибок этого SQL-шаблона.",
		})
	}
	return recommendations
}

func databaseOperationIsRead(operation string) bool {
	switch strings.ToLower(strings.TrimSpace(operation)) {
	case "query", "read", "select", "чтение":
		return true
	default:
		return false
	}
}

func databaseFailureKindSummary(values []NamedValue) string {
	if len(values) == 0 {
		return ""
	}
	var result strings.Builder
	for index, value := range values {
		if index > 0 {
			result.WriteString(", ")
		}
		result.WriteString(databaseFailureDisplayName(value.Name))
		result.WriteString(": ")
		result.WriteString(fmt.Sprint(value.Value))
	}
	return result.String()
}

func databaseHasRelatedCorrelation(stats DatabaseCorrelationStats) bool {
	return stats.HTTPOverlaps > 0 || stats.WorkerOverlaps > 0 ||
		stats.FileIOOverlaps > 0 || stats.GCOverlaps > 0
}

func databaseRelatedCorrelationSummary(stats DatabaseCorrelationStats) string {
	return fmt.Sprintf(
		"HTTP %d; фоновые задачи %d; файловые операции %d; GC %d",
		stats.HTTPOverlaps,
		stats.WorkerOverlaps,
		stats.FileIOOverlaps,
		stats.GCOverlaps,
	)
}

func databaseRelatedThreadCorrelationSummary(
	main, background DatabaseCorrelationStats,
) string {
	return fmt.Sprintf(
		"главный поток: %s; фон: %s",
		databaseRelatedCorrelationSummary(main),
		databaseRelatedCorrelationSummary(background),
	)
}

func databasePhaseEvidence(telemetry DatabaseTelemetryStats) []ProblemEvidence {
	result := make([]ProblemEvidence, 0, 4)
	for _, phase := range [...]struct {
		name  string
		stats DatabasePhaseStats
	}{
		{"Ожидание соединения: верхние 5%", telemetry.PoolWait},
		{"Ожидание блокировки: верхние 5%", telemetry.LockWait},
		{"Выполнение: верхние 5%", telemetry.Execute},
		{"Чтение результата: верхние 5%", telemetry.Materialize},
	} {
		if phase.stats.Samples == 0 {
			continue
		}
		result = append(result, ProblemEvidence{
			Name: phase.name, Observed: fmt.Sprint(microsecondsToMillisecondsCeil(phase.stats.P95DurationUS)),
			Unit: "ms", Sample: u64ptr(phase.stats.Samples), Source: "typed_database_phase",
		})
	}
	return result
}

func (b *problemBuilder) detectCriticalIO(operation IOStats) {
	maxMS := microsecondsToMillisecondsCeil(operation.MaxDurationUS)
	totalMS := microsecondsToMillisecondsCeil(operation.TotalDurationUS)
	classification := classifyIO(operation, b.cfg)
	if !classification.IsProblem() {
		return
	}

	subcategory := "slow_background_io_" + operation.Operation
	title := fmt.Sprintf("%s выполняется слишком долго", ioOperationLabel(operation.Operation))
	impact := 16
	switch classification.Kind {
	case IOClassificationMainThreadSync:
		subcategory = "main_thread_sync_" + operation.Operation
		title = "Синхронизация файла выполняется на главном потоке"
		impact = 32
	case IOClassificationMainThreadLarge:
		subcategory = "main_thread_large_" + operation.Operation
		title = fmt.Sprintf("%s читает или пишет большой объём на главном потоке", ioOperationLabel(operation.Operation))
		impact = 32
	case IOClassificationMainThreadSlow:
		subcategory = "main_thread_io_" + operation.Operation
		title = fmt.Sprintf("%s блокирует главный поток", ioOperationLabel(operation.Operation))
		impact = 32
	case IOClassificationRepeatedFailures:
		subcategory = "io_failures_" + operation.Operation
		title = fmt.Sprintf("%s часто завершается ошибкой", ioOperationLabel(operation.Operation))
		impact = 24
	case IOClassificationBackgroundLarge:
		subcategory = "large_background_io_" + operation.Operation
		title = fmt.Sprintf("%s обрабатывает большой объём за одну операцию", ioOperationLabel(operation.Operation))
		impact = 20
	case IOClassificationSmallOperationStorm:
		subcategory = "small_io_storm_" + operation.Operation
		title = fmt.Sprintf("%s создаёт поток мелких I/O операций", ioOperationLabel(operation.Operation))
		impact = 20
	}

	where := []ProblemLocation{{
		Screen: operation.Screen, Operation: operation.ContextOperation,
		Owner: firstKnown(operation.Source, operation.Owner),
	}}
	if !isUnknownAnalysisValue(operation.Owner) && operation.Owner != operation.Source {
		where = appendUniqueLocation(where, ProblemLocation{
			Screen: operation.Screen, Operation: operation.ContextOperation, Owner: operation.Owner,
		})
	}
	confidence, reasons, limits := problemConfidence(b.summary, operation.Count, 3, true)
	if classification.byteCoverage < 1 {
		limits = append(limits, fmt.Sprintf(
			"Размер известен для %d из %d операций; объём и throughput описывают только покрытую часть.",
			operation.KnownByteOperations,
			operation.Count,
		))
	}
	evidence := []ProblemEvidence{
		{Name: "Максимальная длительность", Observed: fmt.Sprint(maxMS), Unit: "ms", Sample: u64ptr(operation.Count), Source: "typed_io"},
		{Name: "Суммарная длительность", Observed: fmt.Sprint(totalMS), Unit: "ms", Source: "typed_io"},
		{Name: "Неуспешные операции", Observed: fmt.Sprint(operation.Failures), Unit: "events", Denominator: u64ptr(operation.Count), Source: "typed_io"},
		{Name: "Максимальный известный объём", Observed: fmt.Sprint(operation.MaxBytes), Unit: "bytes", Denominator: u64ptr(operation.KnownByteOperations), Source: "typed_io"},
		{Name: "Пик за скользящую секунду", Observed: fmt.Sprint(operation.PeakOperationsPerSecond), Unit: "events/s", Source: "typed_io"},
	}
	thresholdMS := b.cfg.IOBackgroundMS
	if operation.MainThread {
		thresholdMS = b.cfg.IOMainThreadMS
	}
	evidence[0].ExpectedOrThreshold = fmt.Sprintf("< %d ms", thresholdMS)
	peakRate := float64(operation.PeakOperationsPerSecond)
	magnitude := min(25, 6+int(maxMS/maxUint64(thresholdMS, 1))*4)
	if classification.has(ioSignalRepeatedFailures) {
		magnitude = max(magnitude, min(25, 8+int(classification.failureRate/b.cfg.IOFailureRate)*4))
	}
	largeThreshold := b.cfg.IOLargeBackgroundBytes
	if operation.MainThread {
		largeThreshold = b.cfg.IOLargeMainBytes
	}
	if operation.MaxBytes >= largeThreshold {
		magnitude = max(magnitude, min(25, 8+int(operation.MaxBytes/maxUint64(largeThreshold, 1))*3))
	}
	exposure := min(20, 5+int(math.Log2(float64(operation.Count)+1))*3)
	if classification.has(ioSignalSmallOperationStorm) {
		exposure = max(exposure, min(20, 10+int(peakRate/b.cfg.IOStormRate)*2))
	}
	compound := boolScore(classification.signalCount() > 1, 5)
	whatHappened := fmt.Sprintf(
		"%s из %s: %d операций, %d ошибок, граница верхних 5%% %s, максимум %s, пик %d операций/с; известно %d из %d размеров (%d байт).",
		ioOperationLabel(operation.Operation),
		displayUnknown(operation.Source, operation.Owner),
		operation.Count,
		operation.Failures,
		formatMicroseconds(operation.P95DurationUS),
		formatMicroseconds(operation.MaxDurationUS),
		operation.PeakOperationsPerSecond,
		operation.KnownByteOperations,
		operation.Count,
		operation.Bytes,
	)
	detectorID := "io.operation_pressure"
	impactSummary := "Рост задержки, нагрузки на хранилище и конкуренция за I/O"
	action := "Сократить критический путь, объединить мелкие обращения и ограничить объём одной операции"
	var mainThreadBlockedMS *uint64
	if operation.MainThread {
		detectorID = "io.main_thread"
		impactSummary = "Блокировка ввода, пропуски кадров, крупная аллокация и рост риска ANR"
		action = "Перенести целостную операцию с главного потока; чтение/запись выполнять порциями только внутри фоновой задачи"
		mainThreadBlockedMS = nonZeroU64Ptr(totalMS)
	}
	b.add(ProblemFinding{
		DetectorID:      detectorID,
		DetectorVersion: b.cfg.Version, Category: ProblemCategoryIO, Subcategory: subcategory,
		Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: title,
		WhatHappened: whatHappened, Where: where,
		Why: ProblemWhy{
			ClaimLevel: "linked",
			Summary:    "Типизированное событие атомарно связывает целостную I/O-операцию, поток, стабильное место вызова и контекст приложения.",
		},
		Impact:    []string{impactSummary},
		Evidence:  evidence,
		Frequency: &ProblemFrequency{Count: operation.Count, RatePerSec: &peakRate},
		Cost: &ProblemCost{
			MainThreadBlockedMS: mainThreadBlockedMS,
			Bytes:               nonZeroU64Ptr(operation.Bytes),
		},
		PriorityBreakdown: priority(
			impact, magnitude, exposure, locationBreadth(where), compound,
			"задержка и блокировка файловых операций", "длительность, ошибки и объём", "число и пиковая частота", "место вызова и контекст", "сочетание сигналов",
		),
		Recommendations: []ProblemRecommendation{{
			Action:       action,
			Rationale:    "Стабильное место вызова уже локализовано; снижение числа, объёма и длительности обращений уменьшает измеренную нагрузку на хранилище.",
			Verification: "Повторить тот же сценарий и сравнить границу верхних 5%, максимум, ошибки, известный объём и точный пик операций за скользящую секунду.",
		}},
		Limitations: limits,
		Drilldowns:  []ProblemDrilldown{{Label: "Подробно о файловых операциях", Anchor: "io-analysis", Filter: operation.Source}},
	})
}

func (b *problemBuilder) detectMemory() {
	for _, leak := range b.summary.MemoryLeaks {
		if leak.Count == 0 {
			continue
		}
		impact := 18
		if leak.HeapEvidence {
			impact = 32
		}
		if leak.EstimatedRetainedKB >= 16*1024 {
			impact = min(40, impact+6)
		}
		magnitude := min(25, 6+int(math.Log2(float64(leak.EstimatedRetainedKB/1024)+1))*4)
		exposure := min(20, 5+int(leak.Count)*3)
		confidence, reasons, limits := problemConfidence(b.summary, leak.Count, 2, true)
		claim := "hypothesis"
		why := "Объект удерживался дольше ожидаемого; без пути удержания в куче это сигнал, а не доказанная утечка."
		if leak.HeapEvidence {
			confidence, claim, why = "high", "linked", "HPROF подтвердил путь удержания до корня GC."
			reasons = append(reasons, "Есть путь удержания из HPROF и измерение удерживаемого размера.")
		}
		subcategory := "retained_object"
		evidenceSource := "runtime_estimate"
		if leak.HeapEvidence {
			subcategory = "confirmed_leak"
			evidenceSource = "hprof"
		}
		where := []ProblemLocation{{Screen: leak.Screen, Operation: leak.Operation, Owner: leak.Holder, Class: leak.ClassName}}
		b.add(ProblemFinding{
			DetectorID: "memory.retention", DetectorVersion: b.cfg.Version, Category: ProblemCategoryMemory, Subcategory: subcategory,
			Status: "observed", Confidence: confidence, ConfidenceReasons: uniqueStrings(reasons), Title: memoryTitle(leak),
			WhatHappened: fmt.Sprintf("%s удерживался до %d мс; оценка удерживаемого размера — %d КБ; наблюдений — %d.", leak.ClassName, leak.MaxAgeMS, leak.EstimatedRetainedKB, leak.Count), Where: where,
			Why:       ProblemWhy{ClaimLevel: claim, Summary: why, Factors: nonEmptyStrings(leak.GCRoot, leak.HolderField, leak.LeakPattern)},
			Impact:    []string{"Рост памяти, давление GC и риск OOM при накоплении"},
			Evidence:  []ProblemEvidence{{Name: "Возраст удержания", Observed: fmt.Sprint(leak.MaxAgeMS), Unit: "ms", Sample: u64ptr(leak.Count), Source: "retention"}, {Name: "Удерживаемый размер", Observed: fmt.Sprint(leak.EstimatedRetainedKB), Unit: "KB", Source: evidenceSource}},
			Frequency: &ProblemFrequency{Count: leak.Count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(leak.EstimatedRetainedKB)},
			PriorityBreakdown: priority(impact, magnitude, exposure, locationBreadth(where), boolScore(leak.HeapEvidence && leak.EstimatedRetainedKB > 0, 5), "риск OOM", "удерживаемый размер", "повторяемость", "контекст", "путь удержания и размер"),
			Recommendations:   []ProblemRecommendation{{Action: firstNonEmpty(leak.Recommendation, "Разорвать путь удержания и ограничить время жизни владельца"), Rationale: "Устранение пути от корня GC освобождает всё поддерево зависимых объектов.", Verification: "Повторить сценарий, вызвать GC и подтвердить отсутствие объекта и пути удержания в новом HPROF."}},
			Limitations:       append(limits, leak.QualityWarnings...), Drilldowns: []ProblemDrilldown{{Label: "Память", Anchor: "memory-resources", Filter: leak.ClassName}},
		})
	}
	if b.summary.LowMemoryCount > 0 {
		count := uint64(b.summary.LowMemoryCount)
		confidence, reasons, limits := problemConfidence(b.summary, count, 3, true)
		b.add(ProblemFinding{DetectorID: "memory.pressure", DetectorVersion: b.cfg.Version, Category: ProblemCategoryMemory, Subcategory: "low_memory", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Приложение работало при дефиците памяти", WhatHappened: fmt.Sprintf("Дефицит памяти отмечен в %d замерах; максимальный PSS — %d КБ, минимум доступной памяти — %d КБ.", count, b.summary.MemoryMaxKB, b.summary.AvailMemoryMinKB), Why: ProblemWhy{ClaimLevel: "correlated", Summary: "Дефицит памяти и PSS наблюдались в одном прогоне; код, создающий объекты, пока не локализован."}, Impact: []string{"Более частые GC, выгрузка компонентов и риск OOM"}, Evidence: []ProblemEvidence{{Name: "Замеры с дефицитом памяти", Observed: fmt.Sprint(count), Unit: "samples", Source: "memory_context"}, {Name: "Максимальный PSS", Observed: fmt.Sprint(b.summary.MemoryMaxKB), Unit: "KB", Source: "memory_sample"}}, Frequency: &ProblemFrequency{Count: count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(b.summary.MemoryMaxKB)}, PriorityBreakdown: priority(26, 14, min(20, int(count)*4), 2, 3, "дефицит памяти", "PSS и запас доступной памяти", "число замеров", "весь прогон", "дефицит памяти и PSS"), Recommendations: []ProblemRecommendation{{Action: "Записать изменение размера кучи и выделения памяти во времени, затем сократить крупные кеши и буферы", Rationale: "Одного абсолютного значения PSS недостаточно, чтобы назвать участок кода.", Verification: "В длинном повторе сравнить динамику PSS и кучи, паузы GC и минимальный запас доступной памяти."}}, Limitations: append(limits, "Максимальный PSS не доказывает рост памяти; нужна динамика во времени.")})
	}
}

func (b *problemBuilder) detectProcessExit() {
	for _, exit := range b.summary.ProcessExits {
		name, severe := processExitReason(exit.Reason)
		if !severe {
			continue
		}
		when := time.UnixMilli(int64(exit.LatestTimestampUnixMS)).UTC().Format(time.RFC3339)
		confidence := "medium"
		reasons := []string{"ApplicationExitInfo содержит типизированные причину, время, процесс и показатели памяти.", "Событие историческое: оно относится к предыдущему экземпляру процесса."}
		where := []ProblemLocation{{Process: exit.Process}}
		b.add(ProblemFinding{
			DetectorID: "stability.historical_process_exit", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryStability, Subcategory: fmt.Sprintf("historical_exit_%d", exit.Reason), Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        "Android подтвердил предыдущее завершение: " + name,
			WhatHappened: fmt.Sprintf("%s завершился с причиной %s; последнее событие — %s, наблюдений — %d.", displayUnknown(exit.Process, "Процесс"), name, when, exit.Count),
			Where:        where,
			Why:          ProblemWhy{ClaimLevel: "linked", Summary: "Android связал системную причину с конкретным историческим процессом; связь с текущим пользовательским действием не доказана."},
			Impact:       []string{"Потеря пользовательского сценария; для ANR/crash/OOM возможна потеря несохранённых данных"},
			Evidence: []ProblemEvidence{
				{Name: "Причина завершения", Observed: name, Sample: u64ptr(exit.Count), Source: "typed_application_exit_info"},
				{Name: "Время последнего завершения", Observed: when, Source: "typed_application_exit_info"},
				{Name: "PSS при завершении", Observed: fmt.Sprint(exit.MaxPSSKB), Unit: "KB", Source: "typed_application_exit_info"},
				{Name: "RSS при завершении", Observed: fmt.Sprint(exit.MaxRSSKB), Unit: "KB", Source: "typed_application_exit_info"},
			},
			Frequency: &ProblemFrequency{Count: exit.Count}, Cost: &ProblemCost{MemoryKB: nonZeroU64Ptr(exit.MaxPSSKB)},
			PriorityBreakdown: priority(40, 20, min(20, 6+int(exit.Count)*3), locationBreadth(where), 3, "аварийное завершение, ANR или OOM", "системная причина", "исторические события", "процесс", "причина, время и память"),
			Recommendations:   []ProblemRecommendation{{Action: "Сопоставить время с трассой сбоя или ANR и воспроизвести соответствующую операцию", Rationale: "Типизированное завершение подтверждает класс сбоя, но не называет текущую строку кода.", Verification: "Проверить отсутствие новой записи той же причины после исправления и повторной операции."}},
			Limitations:       []string{"ApplicationExitInfo описывает предыдущий процесс и не доказывает, что сбой произошёл в анализируемом прогоне."},
		})
	}
}

func (b *problemBuilder) detectCPU() {
	value, ok := namedValue(b.summary.Gauges, "process.cpu.core_percent_x100")
	if !ok {
		return
	}
	percent := float64(value) / 100
	if percent < b.cfg.ProcessCPUPercent {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, 1, 4, true)
	b.add(ProblemFinding{DetectorID: "cpu.process_saturation", DetectorVersion: b.cfg.Version, Category: ProblemCategoryCPU, Subcategory: "process_cpu", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Процесс длительно нагружает CPU", WhatHappened: fmt.Sprintf("Агрегированная загрузка процесса — %.1f%% одного ядра.", percent), Why: ProblemWhy{ClaimLevel: "unknown", Summary: "Общая загрузка CPU процесса не связывает нагрузку с конкретным методом или задачей."}, Impact: []string{"Конкуренция за CPU, задержки UI, нагрев и расход батареи"}, Evidence: []ProblemEvidence{{Name: "Загрузка CPU процесса", Observed: fmt.Sprintf("%.1f", percent), Unit: "% core", ExpectedOrThreshold: fmt.Sprintf("< %.0f%%", b.cfg.ProcessCPUPercent), Source: "system_sampler_gauge"}}, PriorityBreakdown: priority(22, min(25, int(percent/10)+8), 10, 2, 0, "конкуренция за процессор", "уровень загрузки CPU", "агрегированные замеры", "весь прогон", "нет привязки к задаче"), Recommendations: []ProblemRecommendation{{Action: "Связать всплеск CPU с трассой выполняемых задач и методов", Rationale: "Сигнал всего процесса не называет виновный код.", Verification: "Повторить с записью выполняемых задач и методов и проверить CPU вместе с верхними 5% задержек UI."}}, Limitations: append(limits, "Измерение агрегировано: длительность нагрузки и привязка к отдельным потокам недоступны.")})
}

func (b *problemBuilder) detectRuntimeAnalysis() {
	b.detectAsyncQueues()
	b.detectGCBlocking()
	b.detectStartupLatency()
}

func (b *problemBuilder) detectAsyncQueues() {
	if b.summary.AsyncAnalysis == nil {
		return
	}
	for _, executor := range b.summary.AsyncAnalysis.Executors {
		if executor.WaitSamples < b.cfg.AsyncQueueMinSamples || executor.MaxWaitMS < b.cfg.AsyncQueueWaitMS {
			continue
		}
		saturated := executor.MaxPoolSize > 0 && executor.MaxActiveCount >= executor.MaxPoolSize && executor.MaxQueueDepth > 0
		confidence, reasons, limits := problemConfidence(b.summary, executor.WaitSamples, b.cfg.AsyncQueueMinSamples, true)
		where := []ProblemLocation{{Owner: executor.Name}}
		compound := boolScore(saturated, 4)
		var saturationFactor string
		if saturated {
			saturationFactor = "В тот же период active count достиг размера пула при непустой очереди."
		}
		b.add(ProblemFinding{
			DetectorID: "cpu.async_queue", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryCPU, Subcategory: "executor_queue_wait", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:        fmt.Sprintf("Очередь пула %s задерживает запуск задач", displayUnknown(executor.Name, "неизвестный пул")),
			WhatHappened: fmt.Sprintf("Измерено %d запусков: среднее ожидание %d мс, максимум %d мс; глубина очереди доходила до %d.", executor.WaitSamples, executor.AvgWaitMS, executor.MaxWaitMS, executor.MaxQueueDepth),
			Where:        where,
			Why:          ProblemWhy{ClaimLevel: "linked", Summary: "Обёртка пула напрямую измерила время между постановкой задачи и началом её выполнения.", Factors: nonEmptyStrings(saturationFactor)},
			Impact:       []string{"Задержка фоновых результатов и рост latency зависимых UI/сетевых сценариев"},
			Evidence: []ProblemEvidence{
				{Name: "Максимальное ожидание", Observed: fmt.Sprint(executor.MaxWaitMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.AsyncQueueWaitMS), Sample: u64ptr(executor.WaitSamples), Source: "executor_wrapper_metric"},
				{Name: "Среднее ожидание", Observed: fmt.Sprint(executor.AvgWaitMS), Unit: "ms", Source: "executor_wrapper_metric"},
				{Name: "Максимальная глубина очереди", Observed: fmt.Sprint(executor.MaxQueueDepth), Unit: "events", Source: "executor_wrapper_metric"},
			},
			Frequency:         &ProblemFrequency{Count: executor.WaitSamples},
			PriorityBreakdown: priority(22, min(25, 8+int(executor.MaxWaitMS/b.cfg.AsyncQueueWaitMS)*4), min(20, 5+int(math.Log2(float64(executor.WaitSamples)+1))*3), locationBreadth(where), compound, "задержка результата", "время ожидания в очереди", "число запусков", "пул потоков", "насыщение пула"),
			Recommendations:   []ProblemRecommendation{{Action: "Проверить размер пула, блокирующие задачи и приоритет его очереди", Rationale: "Измерение локализует ожидание до конкретного пула, но не выбирает виновную задачу.", Verification: "Повторить сценарий и подтвердить снижение максимального и среднего ожидания и глубины очереди."}},
			Limitations:       append(limits, "Метрики агрегированы по окнам: максимум точен, среднее взвешено по числу замеров, распределение внутри окна не восстанавливается."),
			Drilldowns:        []ProblemDrilldown{{Label: "Асинхронные очереди", Anchor: "async-analysis", Filter: executor.Name}},
		})
	}
}

func (b *problemBuilder) detectGCBlocking() {
	gc := b.summary.GCAnalysis
	if gc == nil || gc.BlockingCount == 0 || gc.BlockingTimeMS < b.cfg.GCBlockingTimeMS {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, gc.BlockingCount, 1, true)
	b.add(ProblemFinding{
		DetectorID: "memory.gc_blocking", DetectorVersion: b.cfg.Version,
		Category: ProblemCategoryMemory, Subcategory: "blocking_gc", Status: "observed",
		Confidence: confidence, ConfidenceReasons: reasons,
		Title:        "ART зафиксировал заметное время блокирующих GC",
		WhatHappened: fmt.Sprintf("Зафиксировано %d блокирующих GC суммарной длительностью %d мс; всего GC — %d за %d мс.", gc.BlockingCount, gc.BlockingTimeMS, gc.CollectionCount, gc.TotalTimeMS),
		Why:          ProblemWhy{ClaimLevel: "correlated", Summary: fmt.Sprintf("Счётчики ART подтверждают нагрузку от GC в процессе. %d UI-окон с рывками пересеклись по времени с окном измерения GC, но это не доказывает причинность.", gc.JankyUIWindowsNearGC)},
		Impact:       []string{"Паузы всего процесса во время GC могут увеличивать задержку самых медленных кадров и фоновых задач"},
		Evidence: []ProblemEvidence{
			{Name: "Время блокирующих GC", Observed: fmt.Sprint(gc.BlockingTimeMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.GCBlockingTimeMS), Sample: u64ptr(gc.BlockingCount), Source: "art_runtime_stats"},
			{Name: "Блокирующие GC", Observed: fmt.Sprint(gc.BlockingCount), Unit: "events", Source: "art_runtime_stats"},
			{Name: "UI-окна с рывками рядом", Observed: fmt.Sprint(gc.JankyUIWindowsNearGC), Unit: "events", Source: "temporal_window_join"},
		},
		Frequency:         &ProblemFrequency{Count: gc.BlockingCount},
		PriorityBreakdown: priority(28, min(25, 8+int(gc.BlockingTimeMS/b.cfg.GCBlockingTimeMS)*5), min(20, 4+int(gc.BlockingCount)*3), 1, boolScore(gc.JankyUIWindowsNearGC > 0, 4), "паузы процесса", "время блокирующих GC", "число GC", "весь процесс", "совпавшие UI-окна"),
		Recommendations:   []ProblemRecommendation{{Action: "Записать выделения памяти вокруг сценария и сократить часто создаваемые временные объекты", Rationale: "ART показывает нагрузку от GC, но счётчики всего процесса не называют участок кода и тип объектов.", Verification: "Повторить сценарий и сравнить число и время блокирующих GC, скорость выделения памяти и верхние 5% задержек UI."}},
		Limitations:       append(limits, "Время события может соответствовать окончанию окна агрегации; временное совпадение с UI не является доказательством причины."),
		Drilldowns:        []ProblemDrilldown{{Label: "GC и аллокации", Anchor: "gc-analysis"}},
	})
}

func (b *problemBuilder) detectStartupLatency() {
	startup := b.summary.StartupAnalysis
	if startup == nil {
		return
	}
	if startup.ColdResumeSamples > 0 && startup.MaxColdResumeMS >= b.cfg.StartupColdResumeMS {
		confidence, reasons, limits := problemConfidence(b.summary, startup.ColdResumeSamples, 1, true)
		b.add(ProblemFinding{
			DetectorID: "ui.startup_cold", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryUI, Subcategory: "cold_first_resume", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:             "Первое открытие экрана после запуска процесса занимает слишком много времени",
			WhatHappened:      fmt.Sprintf("Первое открытие экрана после запуска процесса: среднее %d мс, максимум %d мс по %d запускам.", startup.AvgColdResumeMS, startup.MaxColdResumeMS, startup.ColdResumeSamples),
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: "Жизненный цикл экрана напрямую измеряет время от запуска наблюдения в процессе до первого вызова onActivityResumed."},
			Impact:            []string{"Пользователь дольше ждёт первого доступного экрана"},
			Evidence:          []ProblemEvidence{{Name: "Максимум первого открытия", Observed: fmt.Sprint(startup.MaxColdResumeMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.StartupColdResumeMS), Sample: u64ptr(startup.ColdResumeSamples), Source: "activity_lifecycle"}},
			Frequency:         &ProblemFrequency{Count: startup.ColdResumeSamples},
			PriorityBreakdown: priority(30, min(25, 10+int(startup.MaxColdResumeMS/b.cfg.StartupColdResumeMS)*5), min(20, 5+int(startup.ColdResumeSamples)*3), 1, 0, "первый экран", "длительность первого открытия", "число запусков", "весь процесс", "один сигнал жизненного цикла"),
			Recommendations:   []ProblemRecommendation{{Action: "Разделить критический путь запуска приложения и первого экрана, затем отложить необязательную инициализацию", Rationale: "Метрика отделяет первое открытие экрана после запуска процесса от последующих переходов.", Verification: "Сравнить время первого открытия на одинаковом устройстве после полного перезапуска процесса."}},
			Limitations:       append(limits, "Это не полное измерение холодного запуска Android и не время до полной отрисовки: отсчёт начинается при запуске наблюдения Jank Hunter."),
			Drilldowns:        []ProblemDrilldown{{Label: "Запуск приложения", Anchor: "startup-analysis"}},
		})
	}
	for _, screen := range startup.Screens {
		if screen.ResumeSamples == 0 || screen.MaxResumeMS < b.cfg.ScreenResumeMS {
			continue
		}
		confidence, reasons, limits := problemConfidence(b.summary, screen.ResumeSamples, 1, true)
		where := []ProblemLocation{{Screen: screen.Screen}}
		b.add(ProblemFinding{
			DetectorID: "ui.screen_resume", DetectorVersion: b.cfg.Version,
			Category: ProblemCategoryUI, Subcategory: "screen_time_to_resume", Status: "observed",
			Confidence: confidence, ConfidenceReasons: reasons,
			Title:             fmt.Sprintf("Экран %s долго переходит от создания к активному состоянию", displayUnknown(screen.Screen, "без атрибуции")),
			WhatHappened:      fmt.Sprintf("Среднее время от создания до активного состояния — %d мс, максимум %d мс по %d наблюдениям.", screen.AvgResumeMS, screen.MaxResumeMS, screen.ResumeSamples),
			Where:             where,
			Why:               ProblemWhy{ClaimLevel: "linked", Summary: "События жизненного цикла Activity напрямую связали создание и переход в активное состояние одного экземпляра экрана."},
			Impact:            []string{"Задержка появления или интерактивности экрана при навигации"},
			Evidence:          []ProblemEvidence{{Name: "Максимум от создания до активного состояния", Observed: fmt.Sprint(screen.MaxResumeMS), Unit: "ms", ExpectedOrThreshold: fmt.Sprintf("< %d ms", b.cfg.ScreenResumeMS), Sample: u64ptr(screen.ResumeSamples), Source: "activity_lifecycle"}},
			Frequency:         &ProblemFrequency{Count: screen.ResumeSamples},
			PriorityBreakdown: priority(24, min(25, 8+int(screen.MaxResumeMS/b.cfg.ScreenResumeMS)*5), min(20, 4+int(screen.ResumeSamples)*3), locationBreadth(where), 0, "переход экрана", "время до активного состояния", "число переходов", "экран", "один сигнал жизненного цикла"),
			Recommendations:   []ProblemRecommendation{{Action: "Проверить синхронную работу между onCreate, onStart и onResume Activity и перенести необязательную подготовку", Rationale: "Метрика локализована до класса экрана.", Verification: "Повторить переход и сравнить среднее и максимальное время до активного состояния."}},
			Limitations:       append(limits, "Интервал включает жизненный цикл и планирование главного потока, но не измеряет время до первого или полного отображения кадра."),
			Drilldowns:        []ProblemDrilldown{{Label: "Запуск и переходы", Anchor: "startup-analysis", Filter: screen.Screen}},
		})
	}
}

func (b *problemBuilder) detectPower() {
	status, ok := namedValue(b.summary.Gauges, "device.thermal.status")
	if !ok || status < b.cfg.ThermalSevereStatus {
		return
	}
	confidence, reasons, limits := problemConfidence(b.summary, 1, 3, true)
	b.add(ProblemFinding{DetectorID: "power.thermal_pressure", DetectorVersion: b.cfg.Version, Category: ProblemCategoryPower, Subcategory: "thermal", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: "Устройство работало при сильном нагреве", WhatHappened: fmt.Sprintf("Состояние нагрева Android достигло %d (порог сильного нагрева: %d).", status, b.cfg.ThermalSevereStatus), Why: ProblemWhy{ClaimLevel: "unknown", Summary: "Состояние нагрева — внешний фактор; оно не доказывает, что приложение вызвало нагрев."}, Impact: []string{"Снижение частот процессора из-за нагрева может усиливать просадки CPU и интерфейса"}, Evidence: []ProblemEvidence{{Name: "Состояние нагрева", Observed: fmt.Sprint(status), Unit: "Android status", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.ThermalSevereStatus), Source: "system_sampler_gauge"}}, PriorityBreakdown: priority(18, min(25, int(status)*4), 8, 2, 0, "ограничение частот", "состояние нагрева", "снимок запуска", "уровень устройства", "причина не связана"), Recommendations: []ProblemRecommendation{{Action: "Повторить операцию на холодном устройстве и сопоставить загрузку процессора и плавность интерфейса", Rationale: "Так отделяется дефект приложения от внешнего влияния нагрева.", Verification: "Сравнить одинаковую операцию при нормальном и сильном нагреве."}}, Limitations: append(limits, "Короткий снимок нагрева не доказывает длительность или источник нагрева.")})
}

func (b *problemBuilder) detectLogSpam() {
	for _, row := range b.summary.LogSpam {
		rate := ratePerSecond(row.Count, b.summary.DurationMS)
		if row.Count < b.cfg.LogSpamMinCount && (rate == nil || *rate < b.cfg.LogSpamRate) {
			continue
		}
		confidence, reasons, limits := problemConfidence(b.summary, row.Count, b.cfg.LogSpamMinCount, true)
		where := []ProblemLocation{{Screen: row.Screen, Operation: row.Operation, Owner: row.Owner}}
		b.add(ProblemFinding{DetectorID: "logs.spam", DetectorVersion: b.cfg.Version, Category: ProblemCategoryLogs, Subcategory: "log_spam", Status: "observed", Confidence: confidence, ConfidenceReasons: reasons, Title: fmt.Sprintf("%s создаёт поток повторяющихся записей журнала", displayUnknown(row.Owner, row.Source)), WhatHappened: fmt.Sprintf("%s.%s записан %d раз%s.", row.Source, row.Level, row.Count, formatOptionalRate(rate)), Where: where, Why: ProblemWhy{ClaimLevel: "linked", Summary: "Автоматический перехват напрямую связал вызовы журналирования с этим источником и контекстом."}, Impact: []string{"Лишние выделения памяти, форматирование и ввод-вывод; полезные сообщения теряются в шуме"}, Evidence: []ProblemEvidence{{Name: "Число сообщений", Observed: fmt.Sprint(row.Count), Unit: "logs", ExpectedOrThreshold: fmt.Sprintf("< %d", b.cfg.LogSpamMinCount), Sample: u64ptr(row.Count), Source: "log_hook"}}, Frequency: &ProblemFrequency{Count: row.Count, RatePerSec: rate}, PriorityBreakdown: priority(12, min(25, 6+int(math.Log2(float64(row.Count)))), min(20, 6+int(math.Log2(float64(row.Count)))), locationBreadth(where), 0, "накладные расходы диагностики", "число", "частота/число", "контекст", "один симптом"), Recommendations: []ProblemRecommendation{{Action: "Удалить запись журнала из часто выполняемого участка или добавить ограничение частоты и объединение", Rationale: "Сокращает накладные расходы и повышает диагностическую ценность журнала.", Verification: "Повторить операцию и проверить число и частоту записей этого источника."}}, Limitations: limits})
	}
}

func (b *problemBuilder) add(f ProblemFinding) {
	f.InvestigationPriority = investigationPriority(f.PriorityBreakdown)
	f.Severity = severityForPriority(f.InvestigationPriority)
	f.Fingerprint = findingFingerprint(f)
	f.ID = "problem-" + f.Fingerprint[:16]
	if f.Status == "" {
		f.Status = "observed"
	}
	b.findings = append(b.findings, f)
}

func (b *problemBuilder) finishFindings() {
	b.findings = uniqueProblemFindings(b.findings)
	for index := range b.findings {
		b.findings[index].RelatedCategories = uniqueStrings(append(
			b.findings[index].RelatedCategories,
			b.findings[index].Category,
		))
		sort.Strings(b.findings[index].RelatedCategories)
	}
	sort.Slice(b.findings, func(i, j int) bool {
		if problemSeverityRank(b.findings[i].Severity) != problemSeverityRank(b.findings[j].Severity) {
			return problemSeverityRank(b.findings[i].Severity) > problemSeverityRank(b.findings[j].Severity)
		}
		if b.findings[i].InvestigationPriority != b.findings[j].InvestigationPriority {
			return b.findings[i].InvestigationPriority > b.findings[j].InvestigationPriority
		}
		if problemConfidenceRank(b.findings[i].Confidence) != problemConfidenceRank(b.findings[j].Confidence) {
			return problemConfidenceRank(b.findings[i].Confidence) > problemConfidenceRank(b.findings[j].Confidence)
		}
		return b.findings[i].Fingerprint < b.findings[j].Fingerprint
	})
}

// uniqueProblemFindings is the last safety boundary for the report. A detector target has one
// fingerprint by design, so overlapping instrumentation must never make the complete problem
// section disappear because two observations resolved to that same target.
func uniqueProblemFindings(findings []ProblemFinding) []ProblemFinding {
	if len(findings) == 0 {
		return findings
	}
	result := make([]ProblemFinding, 0, len(findings))
	positions := make(map[string]int, len(findings))
	for _, finding := range findings {
		index, exists := positions[finding.Fingerprint]
		if !exists {
			positions[finding.Fingerprint] = len(result)
			result = append(result, finding)
			continue
		}
		if preferProblemFinding(finding, result[index]) {
			result[index] = finding
		}
	}
	return result
}

func preferProblemFinding(candidate, current ProblemFinding) bool {
	if candidate.InvestigationPriority != current.InvestigationPriority {
		return candidate.InvestigationPriority > current.InvestigationPriority
	}
	if problemConfidenceRank(candidate.Confidence) != problemConfidenceRank(current.Confidence) {
		return problemConfidenceRank(candidate.Confidence) > problemConfidenceRank(current.Confidence)
	}
	if len(candidate.Evidence) != len(current.Evidence) {
		return len(candidate.Evidence) > len(current.Evidence)
	}
	return candidate.Title < current.Title
}

func (b *problemBuilder) coverage() []CategoryCoverage {
	type coverageDefinition struct {
		id, label  string
		required   []string
		configured bool
		sufficient bool
		available  []string
		action     string
	}
	definitions := []coverageDefinition{
		{ProblemCategoryStability, "Стабильность", []string{"паузы главного потока", "причины завершения процессов"}, collectorConfigured(b.summary, jhlog.CollectorMainThreadStalls) || collectorConfigured(b.summary, jhlog.CollectorProcessExit) || b.summary.StallCount > 0 || len(b.summary.ProcessExits) > 0, true, []string{"паузы главного потока", "причины завершения процессов"}, "Включить сбор пауз и завершений процессов, затем записать активный пользовательский сценарий длительностью не менее 30 секунд."},
		{ProblemCategoryOperations, "Операции приложения", []string{"начало, завершение, бюджет и итог операций"}, b.summary.OperationAnalysis != nil, b.summary.OperationAnalysis != nil && b.summary.OperationAnalysis.Completed >= b.cfg.OperationMinSample && !operationAggregationLimited(b.summary.OperationAnalysis), []string{"типизированный жизненный цикл операций"}, "Добавить измеряемые операции вокруг важных действий, экранов и фоновых работ, затем собрать не менее 20 завершений."},
		{ProblemCategoryUI, "Интерфейс и главный поток", []string{"интервалы наблюдения за интерфейсом", "целевое время и источник кадров", "работа Compose или жизненный цикл экранов"}, collectorConfigured(b.summary, jhlog.CollectorFPS) || collectorConfigured(b.summary, jhlog.CollectorCompose) || b.summary.UIFrames > 0 || b.summary.StartupAnalysis != nil, b.summary.UIFrames >= b.cfg.UIMinFrames || b.summary.StartupAnalysis != nil, []string{"интервалы наблюдения за интерфейсом, границы Compose и жизненный цикл экранов"}, "Включить сбор JankStats и частоты кадров, затем записать не менее 120 кадров."},
		{ProblemCategoryNetwork, "Сеть", []string{"жизненный цикл HTTP и WebSocket"}, b.summary.HTTPCount > 0 || b.summary.WebSocketAnalysis != nil, uint64(b.summary.HTTPCount) >= b.cfg.HTTPMinSample || (b.summary.WebSocketAnalysis != nil && b.summary.WebSocketAnalysis.Opened >= 3), []string{"завершённые HTTP-вызовы и WebSocket-соединения"}, "Подключить jankhunter-okhttp3 и повторить сетевой сценарий."},
		{ProblemCategoryMemory, "Память и сборка мусора", []string{"снимки памяти", "счётчики сборки мусора и выделения памяти ART", "удержания объектов или снимок кучи HPROF"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || collectorConfigured(b.summary, jhlog.CollectorRetainedObjects) || b.summary.MemoryCount > 0 || len(b.summary.MemoryLeaks) > 0 || b.summary.GCAnalysis != nil, b.summary.MemoryCount >= 3 || len(b.summary.MemoryLeaks) > 0 || b.summary.GCAnalysis != nil, []string{"события памяти, сборки мусора и удержаний"}, "Включить сбор памяти; подозрение на утечку проверить с помощью снимка кучи HPROF."},
		{ProblemCategoryIO, "Файлы и база данных", []string{"файловые операции, SQL-вызовы SQLite/Room и вызовы DAO с привязкой к коду"}, collectorConfigured(b.summary, jhlog.CollectorIOTracing) || collectorConfigured(b.summary, jhlog.CollectorRoom) || collectorConfigured(b.summary, jhlog.CollectorDatabase) || b.summary.IOAnalysis != nil || b.summary.DatabaseAnalysis != nil, b.summary.IOAnalysis != nil || b.summary.DatabaseAnalysis != nil || hasSemanticDomain(b.summary, SemanticDomainRoom), []string{"файловые операции, SQL-вызовы SQLite/Room и DAO с привязкой к коду"}, "Включить сбор файловых операций, базы данных и Room, затем выполнить сценарий с БД или хранилищем."},
		{ProblemCategoryCPU, "Процессор и фоновые задачи", []string{"нагрузка процессора", "очереди исполнителей", "выполнения фоновых задач с привязкой к коду"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || collectorConfigured(b.summary, jhlog.CollectorWorker) || hasNamedPrefix(b.summary.Gauges, "process.cpu.") || b.summary.AsyncAnalysis != nil, hasNamedPrefix(b.summary.Gauges, "process.cpu.") || hasSemanticDomain(b.summary, SemanticDomainWorker) || b.summary.AsyncAnalysis != nil, []string{"нагрузка процессора, очереди асинхронных и фоновых задач"}, "Обернуть критичные исполнители и включить системный сбор и сбор фоновых задач, затем выполнить фоновый сценарий."},
		{ProblemCategoryPower, "Энергия и нагрев", []string{"температура и расход батареи", "зарядка и активность приложения"}, collectorConfigured(b.summary, jhlog.CollectorSystemSampler) || hasNamedPrefix(b.summary.Gauges, "device.thermal.") || hasNamedPrefix(b.summary.Gauges, "battery."), hasNamedPrefix(b.summary.Gauges, "device.thermal.") || hasNamedPrefix(b.summary.Gauges, "battery."), []string{"температура и расход батареи"}, "Записать длинный сценарий без зарядки с включённым системным сборщиком."},
		{ProblemCategoryLogs, "Логи", []string{"число вызовов логирования"}, len(b.summary.LogSpam) > 0, len(b.summary.LogSpam) > 0, []string{"число вызовов логирования"}, "Включить запись частого логирования и выполнить соответствующий сценарий."},
		{ProblemCategoryAndroidComponents, "Компоненты Android и IPC", []string{"жизненный цикл Service и BroadcastReceiver", "данные клиента и сервера Binder", "видимость и важность процесса"}, b.summary.AndroidComponents != nil, b.summary.AndroidComponents != nil && b.summary.AndroidComponents.Available && !b.summary.AndroidComponents.Partial, []string{"структурированные события Service, BroadcastReceiver, Binder/AIDL и состояния процесса"}, "Включить сбор компонентов Android для всех процессов сценария и передать полный набор .jhlog одного запуска приложения."},
	}
	if (b.dependencyInjection != nil && b.dependencyInjection.Available) ||
		len(findingsForCoverageCategory(b.findings, ProblemCategoryDependencyInjection)) > 0 {
		definitions = append(definitions, coverageDefinition{
			id: ProblemCategoryDependencyInjection, label: "DI",
			required:   []string{"каталог DI-классов и связей выбранного варианта сборки"},
			configured: true,
			sufficient: true,
			available:  []string{"классы DI и однозначно распознанные связи зависимостей"},
		})
	}
	out := make([]CategoryCoverage, 0, len(definitions))
	for _, definition := range definitions {
		findings := findingsForCoverageCategory(b.findings, definition.id)
		status := "insufficient_data"
		explanation := "Источник данных доступен частично, но наблюдений пока недостаточно для оценки."
		if len(findings) > 0 {
			status, explanation = "problems_found", "Найдены проблемы. Ниже они отсортированы по приоритету расследования."
		} else if definition.id == ProblemCategoryDependencyInjection {
			status = "healthy"
			explanation = "DI-анализ включён; среди зарегистрированных проблем нет сигналов, привязанных к известным DI-классам. Это не доказывает корректность всего графа зависимостей."
		} else if !definition.configured {
			status, explanation = "not_measured", "Обязательный источник данных не подключён, поэтому состояние категории неизвестно."
		} else if collectionEvidenceDegraded(b.summary.CollectionQuality) {
			status, explanation = "collection_degraded", "Данных недостаточно, чтобы подтвердить отсутствие проблем в этой категории."
		} else if !definition.sufficient {
			status, explanation = "insufficient_data", "Сбор включён, но наблюдений пока недостаточно для оценки."
		} else if b.summary.DurationMS >= 30_000 {
			status, explanation = "healthy", "В записанном сценарии опасные пороги не превышены."
		}
		coverage := CategoryCoverage{Category: definition.id, Label: definition.label, Status: status, FindingCount: len(findings), RequiredEvidence: definition.required, Explanation: explanation}
		if definition.configured {
			coverage.AvailableEvidence = definition.available
		} else {
			coverage.MissingEvidence = definition.required
			coverage.NextAction = definition.action
		}
		if len(findings) > 0 {
			coverage.WorstSeverity = findings[0].Severity
		}
		out = append(out, coverage)
	}
	return out
}

func collectionEvidenceDegraded(quality CollectionQuality) bool {
	if quality.Complete {
		return false
	}
	if quality.SegmentsWithQuality == 0 &&
		quality.AcceptedEvents == 0 &&
		quality.WrittenEvents == 0 &&
		quality.DamagedSegments == 0 {
		return false
	}
	return !quality.ExactAdmission ||
		!quality.ChainValid ||
		!quality.CounterInvariantsValid ||
		!quality.QualityProgressionValid ||
		quality.DamagedSegments > 0 ||
		quality.KnownLostEvents > 0 ||
		quality.ControlFailures > 0 ||
		quality.BoundedEvidenceLoss > 0 ||
		quality.OtherEvidenceLoss > 0
}

func summarizeProblems(findings []ProblemFinding, coverage []CategoryCoverage) ProblemSummary {
	s := ProblemSummary{SchemaVersion: ProblemSchemaVersion, Total: len(findings), SignalTotal: len(findings)}
	for _, finding := range findings {
		switch finding.Severity {
		case "critical":
			s.Critical++
		case "high":
			s.High++
		case "medium":
			s.Medium++
		case "low":
			s.Low++
		default:
			s.Info++
		}
	}
	for _, category := range coverage {
		count := 0
		for _, finding := range findings {
			if problemFindingHasCategory(finding, category.Category) {
				count++
			}
		}
		s.ByCategory = append(s.ByCategory, ProblemCount{Category: category.Category, Count: count})
		if category.Status != "healthy" && category.Status != "problems_found" {
			s.Unchecked++
		}
	}
	if s.Total > 0 {
		s.Verdict = "problems_found"
	} else if s.Unchecked > 0 {
		s.Verdict = "incomplete"
	} else {
		s.Verdict = "clean"
	}
	s.Headline = problemSummaryHeadline(s)
	return s
}

func problemSummaryHeadline(summary ProblemSummary) string {
	if summary.Total > 0 {
		if summary.SignalTotal > summary.Total {
			return fmt.Sprintf(
				"Найдено инцидентов: %d; объединено связанных сигналов: %d. Максимальный приоритет расследования — %s.",
				summary.Total,
				summary.SignalTotal,
				problemSummaryWorstPriority(summary),
			)
		}
		return fmt.Sprintf("Найдено проблем: %d. Максимальный приоритет расследования — %s.", summary.Total, problemSummaryWorstPriority(summary))
	}
	if summary.Unchecked > 0 {
		return fmt.Sprintf("Проблем не найдено, но %d категорий не проверены полностью.", summary.Unchecked)
	}
	return "В записанном сценарии значимых проблем не обнаружено."
}

func problemSummaryWorstPriority(summary ProblemSummary) string {
	switch {
	case summary.Critical > 0:
		return "критичный"
	case summary.High > 0:
		return "высокий"
	case summary.Medium > 0:
		return "средний"
	case summary.Low > 0:
		return "низкий"
	default:
		return "информационный"
	}
}

func detectorRegistry(cfg ProblemDetectorConfig) []DetectorMetadata {
	return []DetectorMetadata{
		{ID: "stability.historical_process_exit", Version: cfg.Version, Category: ProblemCategoryStability, Title: "Историческое завершение процесса", MinimumSample: 1, RequiredSignals: []string{"ApplicationExitInfo"}},
		{ID: "operations.health", Version: cfg.Version, Category: ProblemCategoryOperations, Title: "Нарушение бюджета или неуспешный итог операции", MinimumSample: cfg.OperationMinSample, RequiredSignals: []string{"operation lifecycle"}, Thresholds: []DetectorThreshold{{"budget_breach_rate", cfg.OperationBudgetBreachRate * 100, "%"}, {"failure_rate", cfg.OperationFailureRate * 100, "%"}}},
		{ID: "stability.main_thread_stall", Version: cfg.Version, Category: ProblemCategoryStability, Title: "Остановка главного потока", MinimumSample: 1, RequiredSignals: []string{"main-thread stall"}, Thresholds: []DetectorThreshold{{"stall", float64(cfg.StallMS), "ms"}, {"high", float64(cfg.StallHighMS), "ms"}}},
		{ID: "ui.jank_tail", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Jank и длинный tail кадров", MinimumSample: cfg.UIMinFrames, RequiredSignals: []string{"UI window"}, Thresholds: []DetectorThreshold{{"jank_rate", cfg.UIJankRate, "%"}, {"frame_tail", float64(cfg.UIFrameTailMS), "ms"}}},
		{ID: "ui.compose_work", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Тяжёлая или частая Compose-работа", MinimumSample: 3, RequiredSignals: []string{"Compose boundary"}, Thresholds: []DetectorThreshold{{"frame_budget", float64(composeDefaultFrameBudgetMS), "ms"}, {"frequent", float64(composeFrequentMinCount), "events"}}},
		{ID: "ui.startup_cold", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Медленный первый resume процесса", MinimumSample: 1, RequiredSignals: []string{"first Activity resume"}, Thresholds: []DetectorThreshold{{"first_resume", float64(cfg.StartupColdResumeMS), "ms"}}},
		{ID: "ui.screen_resume", Version: cfg.Version, Category: ProblemCategoryUI, Title: "Медленный create-to-resume экрана", MinimumSample: 1, RequiredSignals: []string{"Activity create/resume"}, Thresholds: []DetectorThreshold{{"screen_resume", float64(cfg.ScreenResumeMS), "ms"}}},
		{ID: "network.route_health", Version: cfg.Version, Category: ProblemCategoryNetwork, Title: "Медленный/ошибочный/частый route", MinimumSample: cfg.HTTPMinSample, RequiredSignals: []string{"typed HTTP"}, Thresholds: []DetectorThreshold{{"slow_p95", float64(cfg.HTTPSlowMS), "ms"}, {"failure_rate", cfg.HTTPFailureRate * 100, "%"}, {"storm_rate", cfg.HTTPStormRate, "requests/s"}}},
		{ID: "network.websocket_health", Version: cfg.Version, Category: ProblemCategoryNetwork, Title: "Нестабильный WebSocket", MinimumSample: 3, RequiredSignals: []string{"typed WebSocket lifecycle"}, Thresholds: []DetectorThreshold{{"failure_rate", 25, "%"}, {"reconnects", 3, "events"}, {"slow_connect_p95", 1500, "ms"}, {"rapid_churn_lifetime_p95", 5000, "ms"}}},
		{ID: "memory.retention", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Удержание памяти", MinimumSample: 1, RequiredSignals: []string{"retention or HPROF"}},
		{ID: "memory.pressure", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Дефицит памяти", MinimumSample: 3, RequiredSignals: []string{"memory/context samples"}},
		{ID: "memory.gc_blocking", Version: cfg.Version, Category: ProblemCategoryMemory, Title: "Блокирующий GC", MinimumSample: 1, RequiredSignals: []string{"ART GC counters"}, Thresholds: []DetectorThreshold{{"blocking_time", float64(cfg.GCBlockingTimeMS), "ms"}}},
		{ID: "io.main_thread", Version: cfg.Version, Category: ProblemCategoryIO, Title: "I/O на главном потоке", MinimumSample: 1, RequiredSignals: []string{"attributed I/O with source"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.IOMainThreadMS), "ms"}, {"large_operation", float64(cfg.IOLargeMainBytes), "bytes"}}},
		{ID: "io.operation_pressure", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный, ошибочный или частый I/O", MinimumSample: 1, RequiredSignals: []string{"attributed I/O with source"}, Thresholds: []DetectorThreshold{{"background_slow", float64(cfg.IOBackgroundMS), "ms"}, {"storm_rate", cfg.IOStormRate, "events/s"}, {"failure_rate", cfg.IOFailureRate * 100, "%"}, {"large_operation", float64(cfg.IOLargeBackgroundBytes), "bytes"}}},
		{ID: "io.database_repeated_in_scope", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Повтор SQL внутри одной границы сценария", MinimumSample: 2, RequiredSignals: []string{"нормализованный SQL-шаблон", "идентификатор операции или транзакции"}, Thresholds: []DetectorThreshold{{"repeated_calls_per_scope", databaseScenarioRepeatCalls, "events"}}},
		{ID: "io.database_plan_evidence", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Подтверждённый шаг плана SQL-запроса", MinimumSample: 1, RequiredSignals: []string{"предоставленный разработчиком очищенный план SQL-запроса", "совпадающий нормализованный SQL-шаблон"}},
		{ID: "io.database_calls", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный, частый или ошибочный SQL-вызов", MinimumSample: 5, RequiredSignals: []string{"typed SQLite/Room invocation"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.DatabaseMainThreadMS), "ms"}, {"background_slow", float64(cfg.DatabaseBackgroundMS), "ms"}, {"storm_rate", float64(cfg.DatabaseStormRate), "events/s"}, {"rapid_repeats", float64(cfg.DatabaseRapidRepeatCount), "events"}, {"failure_rate", cfg.DatabaseFailureRate * 100, "%"}}},
		{ID: "io.database_transaction", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Длинная, неуспешная или незавершённая транзакция БД", MinimumSample: 1, RequiredSignals: []string{"typed database transaction lifecycle"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.DatabaseTransactionMainMS), "ms"}, {"background_slow", float64(cfg.DatabaseTransactionBackgroundMS), "ms"}, {"many_statements", float64(cfg.DatabaseTransactionStatements), "events"}}},
		{ID: "io.room_main_thread", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Room DAO на главном потоке", MinimumSample: 1, RequiredSignals: []string{"Room DAO boundary"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(cfg.IOMainThreadMS), "ms"}}},
		{ID: "io.room_pressure", Version: cfg.Version, Category: ProblemCategoryIO, Title: "Медленный или частый Room DAO", MinimumSample: 1, RequiredSignals: []string{"Room DAO boundary"}, Thresholds: []DetectorThreshold{{"background_slow", float64(cfg.IOBackgroundMS), "ms"}, {"storm_rate", cfg.IOStormRate, "events/s"}}},
		{ID: "cpu.process_saturation", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Высокая загрузка CPU", MinimumSample: 4, RequiredSignals: []string{"process CPU"}, Thresholds: []DetectorThreshold{{"process_cpu", cfg.ProcessCPUPercent, "% core"}}},
		{ID: "cpu.worker_execution", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Долгая, повторная или неуспешная Worker-задача", MinimumSample: 1, RequiredSignals: []string{"Worker boundary"}, Thresholds: []DetectorThreshold{{"long_execution", float64(workerLongExecutionMS), "ms"}, {"repeated", float64(workerRepeatedMinCount), "events"}}},
		{ID: "cpu.async_queue", Version: cfg.Version, Category: ProblemCategoryCPU, Title: "Ожидание в очереди Executor", MinimumSample: cfg.AsyncQueueMinSamples, RequiredSignals: []string{"wrapped Executor metrics"}, Thresholds: []DetectorThreshold{{"queue_wait", float64(cfg.AsyncQueueWaitMS), "ms"}}},
		{ID: "power.thermal_pressure", Version: cfg.Version, Category: ProblemCategoryPower, Title: "Thermal pressure", MinimumSample: 3, RequiredSignals: []string{"thermal status"}, Thresholds: []DetectorThreshold{{"severe", float64(cfg.ThermalSevereStatus), "Android status"}}},
		{ID: "logs.spam", Version: cfg.Version, Category: ProblemCategoryLogs, Title: "Спам логами", MinimumSample: cfg.LogSpamMinCount, RequiredSignals: []string{"log hook"}, Thresholds: []DetectorThreshold{{"count", float64(cfg.LogSpamMinCount), "logs"}, {"rate", cfg.LogSpamRate, "logs/s"}}},
		{ID: "android.service.callback_failure", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Ошибка callback службы", MinimumSample: 1, RequiredSignals: []string{"typed Service callback"}},
		{ID: "android.service.timeout", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Таймаут службы", MinimumSample: 1, RequiredSignals: []string{"typed Service onTimeout"}},
		{ID: "android.service.callback_slow", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Долгий callback службы", MinimumSample: 1, RequiredSignals: []string{"typed Service callback"}, Thresholds: []DetectorThreshold{{"slow_callback", float64(serviceSlowCallbackUS / 1_000), "ms"}}},
		{ID: "android.receiver.async_deadline_risk", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "BroadcastReceiver близок к системному дедлайну", MinimumSample: 1, RequiredSignals: []string{"typed BroadcastReceiver async lifecycle"}, Thresholds: []DetectorThreshold{{"deadline_risk", float64(receiverDeadlineRiskUS / 1_000), "ms"}}},
		{ID: "android.receiver.sync_slow", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Синхронная работа в BroadcastReceiver", MinimumSample: 1, RequiredSignals: []string{"typed BroadcastReceiver lifecycle"}, Thresholds: []DetectorThreshold{{"sync_work", float64(receiverSyncSlowUS / 1_000), "ms"}}},
		{ID: "android.receiver.failure", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Ошибка BroadcastReceiver", MinimumSample: 1, RequiredSignals: []string{"typed BroadcastReceiver lifecycle"}},
		{ID: "android.binder.main_thread_slow", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Медленный Binder-вызов на главном потоке", MinimumSample: 1, RequiredSignals: []string{"typed Binder client transaction"}, Thresholds: []DetectorThreshold{{"main_thread_slow", float64(binderMainThreadSlowUS / 1_000), "ms"}}},
		{ID: "android.binder.failure", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Ошибка Binder-транзакции", MinimumSample: 1, RequiredSignals: []string{"typed Binder transaction outcome"}},
		{ID: "android.binder.unhandled", Version: cfg.Version, Category: ProblemCategoryAndroidComponents, Title: "Необработанный transaction code", MinimumSample: 1, RequiredSignals: []string{"typed Binder transaction outcome"}},
	}
}

func validateProblemReport(report ProblemReport) error {
	categories := map[string]struct{}{
		ProblemCategoryStability: {}, ProblemCategoryOperations: {}, ProblemCategoryUI: {}, ProblemCategoryNetwork: {}, ProblemCategoryMemory: {},
		ProblemCategoryIO: {}, ProblemCategoryCPU: {}, ProblemCategoryPower: {}, ProblemCategoryLogs: {},
		ProblemCategoryAndroidComponents: {}, ProblemCategoryDependencyInjection: {},
	}
	detectors := map[string]struct{}{}
	for _, detector := range report.Registry {
		if _, ok := categories[detector.Category]; !ok {
			return fmt.Errorf("detector %q has unknown category %q", detector.ID, detector.Category)
		}
		if detector.ID == "" || detector.Version == "" {
			return fmt.Errorf("detector metadata is incomplete")
		}
		if _, exists := detectors[detector.ID]; exists {
			return fmt.Errorf("duplicate detector %q", detector.ID)
		}
		detectors[detector.ID] = struct{}{}
	}
	knownSeverity := map[string]struct{}{"info": {}, "low": {}, "medium": {}, "high": {}, "critical": {}}
	knownUnits := map[string]struct{}{"": {}, "requests": {}, "attempts": {}, "ms": {}, "%": {}, "requests/s": {}, "events": {}, "events/s": {}, "events/scope": {}, "calls": {}, "calls/boundary": {}, "bytes": {}, "KB": {}, "samples": {}, "% core": {}, "Android status": {}, "logs": {}}
	fingerprints := map[string]struct{}{}
	for _, finding := range report.Problems {
		if _, ok := detectors[finding.DetectorID]; !ok {
			return fmt.Errorf("finding %q uses unknown detector %q", finding.ID, finding.DetectorID)
		}
		if _, ok := categories[finding.Category]; !ok {
			return fmt.Errorf("finding %q has unknown category %q", finding.ID, finding.Category)
		}
		if _, ok := knownSeverity[finding.Severity]; !ok {
			return fmt.Errorf("finding %q has invalid severity %q", finding.ID, finding.Severity)
		}
		if finding.Confidence != "low" && finding.Confidence != "medium" && finding.Confidence != "high" {
			return fmt.Errorf("finding %q has invalid confidence %q", finding.ID, finding.Confidence)
		}
		if finding.Why.ClaimLevel != "linked" && finding.Why.ClaimLevel != "correlated" && finding.Why.ClaimLevel != "hypothesis" && finding.Why.ClaimLevel != "unknown" {
			return fmt.Errorf("finding %q has invalid claim level %q", finding.ID, finding.Why.ClaimLevel)
		}
		if err := validateInvestigationPriority(finding); err != nil {
			return fmt.Errorf("finding %q has invalid investigation priority: %w", finding.ID, err)
		}
		for _, evidence := range finding.Evidence {
			if _, ok := knownUnits[evidence.Unit]; !ok {
				return fmt.Errorf("finding %q has unknown unit %q", finding.ID, evidence.Unit)
			}
		}
		if _, exists := fingerprints[finding.Fingerprint]; exists {
			return fmt.Errorf("duplicate finding fingerprint %q", finding.Fingerprint)
		}
		fingerprints[finding.Fingerprint] = struct{}{}
	}
	return nil
}

func priority(impact, magnitude, exposure, breadth, compounding int, impactWhy, magnitudeWhy, exposureWhy, breadthWhy, compoundWhy string) []ProblemPriorityComponent {
	return []ProblemPriorityComponent{
		{Component: "impact", Score: clamp(impact, 0, 40), Maximum: 40, Explanation: impactWhy},
		{Component: "magnitude", Score: clamp(magnitude, 0, 25), Maximum: 25, Explanation: magnitudeWhy},
		{Component: "exposure", Score: clamp(exposure, 0, 20), Maximum: 20, Explanation: exposureWhy},
		{Component: "breadth", Score: clamp(breadth, 0, 10), Maximum: 10, Explanation: breadthWhy},
		{Component: "compounding", Score: clamp(compounding, 0, 5), Maximum: 5, Explanation: compoundWhy},
	}
}

func investigationPriority(parts []ProblemPriorityComponent) int {
	total := 0
	for _, part := range parts {
		total += part.Score
	}
	return clamp(total, 0, 100)
}

func validateInvestigationPriority(finding ProblemFinding) error {
	if finding.InvestigationPriority < 0 || finding.InvestigationPriority > 100 || len(finding.PriorityBreakdown) != 5 {
		return fmt.Errorf("expected a value in 0..100 and five components")
	}
	expected := [...]struct {
		name    string
		maximum int
	}{{"impact", 40}, {"magnitude", 25}, {"exposure", 20}, {"breadth", 10}, {"compounding", 5}}
	for index, component := range finding.PriorityBreakdown {
		if component.Component != expected[index].name || component.Maximum != expected[index].maximum ||
			component.Score < 0 || component.Score > component.Maximum {
			return fmt.Errorf("component %d does not match %s 0..%d", index, expected[index].name, expected[index].maximum)
		}
	}
	if calculated := investigationPriority(finding.PriorityBreakdown); finding.InvestigationPriority != calculated {
		return fmt.Errorf("value %d does not equal component sum %d", finding.InvestigationPriority, calculated)
	}
	return nil
}

func severityForPriority(score int) string {
	switch {
	case score >= 80:
		return "critical"
	case score >= 60:
		return "high"
	case score >= 35:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "info"
	}
}
func problemSeverityRank(value string) int {
	switch value {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 0
	}
}
func problemConfidenceRank(value string) int {
	switch value {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 0
	}
}

func capProblemConfidence(value, maximum string) string {
	if problemConfidenceRank(value) > problemConfidenceRank(maximum) {
		return maximum
	}
	return value
}

func problemConfidence(summary Summary, sample, minimum uint64, direct bool) (string, []string, []string) {
	confidence := "high"
	reasons := []string{}
	limits := []string{}
	if direct {
		reasons = append(reasons, "Симптом записан типизированным событием во время выполнения приложения.")
	}
	if sample < minimum {
		confidence = "low"
		reasons = append(reasons, fmt.Sprintf("Выборка %d меньше минимума %d.", sample, minimum))
		limits = append(limits, "Малая выборка: пока нельзя надёжно оценить типичную задержку и частоту вызовов.")
	} else {
		reasons = append(reasons, fmt.Sprintf("Выборка %d достигает минимума %d.", sample, minimum))
	}
	if collectionEvidenceDegraded(summary.CollectionQuality) {
		confidence = "low"
		reasons = append(reasons, "Охват измерений неполный, поэтому абсолютные количества считаются нижней оценкой.")
		limits = append(limits, "Используйте сохранённые места в коде для локализации, а частоту проблемы подтвердите повторным прогоном.")
	} else if confidence == "high" && !summary.AnalysisInputs.Complete {
		confidence = "medium"
		reasons = append(reasons, "Не все дополнительные материалы для анализа доступны.")
	}
	return confidence, uniqueStrings(reasons), uniqueStrings(limits)
}

func findingFingerprint(f ProblemFinding) string {
	// Fingerprints identify the underlying detector target. Subcategory and detector version are
	// deliberately excluded so a slow route can become a compound storm without looking "new".
	parts := []string{f.DetectorID, f.Category}
	if f.DetectorID == "stability.historical_process_exit" || strings.HasPrefix(f.DetectorID, "io.") {
		parts = append(parts, f.Subcategory)
	}
	locationKeys := make([]string, len(f.Where))
	for index, location := range f.Where {
		locationKeys[index] = locationKey(location)
	}
	sort.Strings(locationKeys)
	parts = append(parts, locationKeys...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func locationKey(v ProblemLocation) string {
	return strings.ToLower(strings.Join([]string{v.Process, v.Screen, v.Operation, v.Route, v.Owner, v.Class, v.Method}, "\x1f"))
}
func locationBreadth(where []ProblemLocation) int {
	seen := map[string]struct{}{}
	for _, item := range where {
		seen[locationKey(item)] = struct{}{}
	}
	return min(10, max(1, len(seen)*2))
}
func appendUniqueLocation(values []ProblemLocation, value ProblemLocation) []ProblemLocation {
	key := locationKey(value)
	for _, item := range values {
		if locationKey(item) == key {
			return values
		}
	}
	return append(values, value)
}

func networkProblemLocations(summary Summary) map[string][]ProblemLocation {
	locations := make(map[string][]ProblemLocation, len(summary.Routes))
	for _, route := range summary.Routes {
		locations[route.Route] = []ProblemLocation{{Route: route.Route, Owner: route.OwnerSample}}
	}
	if summary.NetworkAnalysis != nil {
		for _, call := range summary.NetworkAnalysis.Calls {
			locations[call.Route] = appendUniqueLocation(locations[call.Route], ProblemLocation{
				Screen: call.Screen, Operation: call.Operation,
				Route: call.Route, Owner: firstKnown(call.Initiator, call.Owner),
			})
		}
	}
	for _, context := range summary.SignalContexts {
		if context.RouteSample == "" {
			continue
		}
		locations[context.RouteSample] = appendUniqueLocation(locations[context.RouteSample], ProblemLocation{
			Screen: context.Screen, Operation: context.Operation,
			Route: context.RouteSample, Owner: context.Owner,
		})
	}
	return locations
}
func findingsForCategory(values []ProblemFinding, category string) []ProblemFinding {
	out := []ProblemFinding{}
	for _, value := range values {
		if value.Category == category {
			out = append(out, value)
		}
	}
	return out
}

func findingsForCoverageCategory(values []ProblemFinding, category string) []ProblemFinding {
	out := make([]ProblemFinding, 0)
	for _, value := range values {
		if problemFindingHasCategory(value, category) {
			out = append(out, value)
		}
	}
	return out
}

func problemFindingHasCategory(finding ProblemFinding, category string) bool {
	if finding.Category == category {
		return true
	}
	for _, related := range finding.RelatedCategories {
		if related == category {
			return true
		}
	}
	return false
}
func ratePerSecond(count, durationMS uint64) *float64 {
	if durationMS == 0 {
		return nil
	}
	value := float64(count) * 1000 / float64(durationMS)
	return &value
}
func problemRatio(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func u64ptr(v uint64) *uint64 { return &v }
func nonZeroU64Ptr(v uint64) *uint64 {
	if v == 0 {
		return nil
	}
	return &v
}
func formatPercent(v float64) string { return fmt.Sprintf("%.1f", v) }
func formatOptionalRate(rate *float64) string {
	if rate == nil {
		return ""
	}
	return fmt.Sprintf(" (%.2f/с)", *rate)
}
func displayUnknown(v, fallback string) string {
	return datavalue.HumanUnknown(v, fallback)
}
func boolCount(values ...bool) int {
	total := 0
	for _, value := range values {
		if value {
			total++
		}
	}
	return total
}
func boolScore(value bool, score int) int {
	if value {
		return score
	}
	return 0
}
func clamp(v, low, high int) int { return min(high, max(low, v)) }
func nonEmptyStrings(values ...string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}
func namedValue(values []NamedValue, name string) (uint64, bool) {
	for _, value := range values {
		if value.Name == name {
			return value.Value, true
		}
	}
	return 0, false
}
func hasNamedPrefix(values []NamedValue, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value.Name, prefix) {
			return true
		}
	}
	return false
}

func collectorConfigured(summary Summary, flag jhlog.CollectorFlag) bool {
	return summary.CollectorSessions > 0 && summary.CollectorFlagsAll&uint64(flag) != 0
}

func microsecondsToMillisecondsCeil(value uint64) uint64 {
	return value/1_000 + boolToUint64(value%1_000 != 0)
}

func formatMicroseconds(value uint64) string {
	if value%1_000 == 0 {
		return fmt.Sprintf("%d ms", value/1_000)
	}
	return fmt.Sprintf("%.2f ms", float64(value)/1_000)
}

func boolToUint64(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func formatFrameDeadline(valueUS uint64) string {
	if valueUS == 0 {
		return "неоднородное или не записано"
	}
	return fmt.Sprintf("%.3f", float64(valueUS)/1_000)
}

func problemFrameSourceLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "mixed":
		return "смешанный"
	case "choreographer":
		return "Choreographer"
	case "jankstats":
		return "JankStats"
	case "", "unknown":
		return "не записан"
	default:
		return value
	}
}

func ioOperationLabel(operation string) string {
	label := map[string]string{
		"file_read": "Чтение файла", "file_write": "Запись файла", "file_sync": "Синхронизация файла",
		"content_read": "Чтение ContentProvider", "content_write": "Запись ContentProvider",
	}[operation]
	return firstNonEmpty(label, operation, "I/O операция")
}
func processExitReason(reason uint64) (string, bool) {
	switch reason {
	case 3:
		return "low memory / OOM pressure", true
	case 4:
		return "Java crash", true
	case 5:
		return "native crash", true
	case 6:
		return "ANR", true
	case 7:
		return "initialization failure", true
	case 9:
		return "excessive resource usage", true
	default:
		return fmt.Sprintf("reason %d", reason), false
	}
}
func memoryTitle(leak MemoryLeakSuspect) string {
	if leak.HeapEvidence {
		return fmt.Sprintf("%s подтверждённо удерживается до GC root", displayUnknown(leak.ClassName, "Объект"))
	}
	return fmt.Sprintf("%s подозрительно долго удерживается", displayUnknown(leak.ClassName, "Объект"))
}
func networkSubcategory(slow, failed, storm bool) string {
	if storm && slow {
		return "slow_storm"
	}
	if storm {
		return "request_storm"
	}
	if failed {
		return "failure_rate"
	}
	return "slow_route"
}
func networkFactors(route RouteStats, slow, failed, storm bool) []string {
	out := []string{}
	if slow {
		out = append(out, "Высокая задержка среди самых медленных вызовов")
	}
	if failed {
		out = append(out, "Высокая доля ошибок")
	}
	if storm {
		out = append(out, "Высокий секундный пик завершений")
	}
	if phase, ok := dominantHTTPPhase(route.Phases); ok {
		out = append(out, fmt.Sprintf("Доминирующая измеренная фаза — %s, граница верхних 5%% %d мс", httpPhaseProblemLabel(phase.Name), phase.P95MS))
	}
	if route.Retries > 0 {
		out = append(out, fmt.Sprintf("%d дополнительных попыток запроса без учёта перенаправлений", route.Retries))
	}
	if route.ConnectFailures > 0 || route.TLSFailures > 0 {
		out = append(out, fmt.Sprintf("Ошибки попыток соединения: обычное соединение — %d, защищённое TLS-соединение — %d", route.ConnectFailures, route.TLSFailures))
	}
	return out
}
func networkWhat(route RouteStats, failures, count uint64, slow, storm bool, minimumSample uint64) string {
	parts := []string{fmt.Sprintf("Запрос выполнился %s", russianTimes(count))}
	if slow {
		switch {
		case count == 1:
			parts = append(parts, fmt.Sprintf("единственный вызов занял %d мс", route.P95MS))
		case count < minimumSample:
			parts = append(parts, fmt.Sprintf("задержка в верхней части небольшой выборки достигла %d мс", route.P95MS))
		default:
			parts = append(parts, fmt.Sprintf("95%% вызовов завершились не дольше %d мс", route.P95MS))
		}
	}
	if failures > 0 {
		parts = append(parts, fmt.Sprintf("с ошибкой завершилось %s", russianRequestCount(failures)))
	}
	if storm {
		parts = append(parts, fmt.Sprintf("в пике завершалось %d запросов в секунду — это выше допустимого уровня", route.PeakRequestsPerSecond))
	}
	if phase, ok := dominantHTTPPhase(route.Phases); ok {
		parts = append(parts, fmt.Sprintf("самая длинная фаза среди верхних 5%% задержек — %s (%d мс)", httpPhaseProblemLabel(phase.Name), phase.P95MS))
	}
	if route.Retries > 0 {
		parts = append(parts, fmt.Sprintf("зафиксировано %d повторных попыток без учета редиректов", route.Retries))
	}
	return strings.Join(parts, "; ") + "."
}

func dominantHTTPPhase(phases []HTTPPhaseStats) (HTTPPhaseStats, bool) {
	var dominant HTTPPhaseStats
	found := false
	for _, phase := range phases {
		if phase.SampleCount == 0 || (found && phase.P95MS <= dominant.P95MS) {
			continue
		}
		dominant = phase
		found = true
	}
	return dominant, found
}

func httpPhaseProblemLabel(name string) string {
	switch name {
	case "queue":
		return "очередь"
	case "dns":
		return "DNS"
	case "connect":
		return "соединение"
	case "tls":
		return "TLS"
	case "request":
		return "отправка запроса"
	case "ttfb":
		return "TTFB"
	case "response":
		return "получение ответа"
	default:
		return name
	}
}

func russianTimes(count uint64) string {
	if count%10 == 1 && count%100 != 11 {
		return fmt.Sprintf("%d раз", count)
	}
	if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		return fmt.Sprintf("%d раза", count)
	}
	return fmt.Sprintf("%d раз", count)
}

func russianRequestCount(count uint64) string {
	word := "запросов"
	if count%10 == 1 && count%100 != 11 {
		word = "запрос"
	} else if count%10 >= 2 && count%10 <= 4 && (count%100 < 12 || count%100 > 14) {
		word = "запроса"
	}
	return fmt.Sprintf("%d %s", count, word)
}
