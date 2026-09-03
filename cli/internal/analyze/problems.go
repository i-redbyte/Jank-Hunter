package analyze

import (
	"fmt"
	"sort"
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
