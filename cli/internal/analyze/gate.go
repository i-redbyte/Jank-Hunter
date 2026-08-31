package analyze

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func LoadThresholdConfig(path string) (ThresholdConfig, error) {
	if path == "" {
		return ThresholdConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ThresholdConfig{}, err
	}
	var config ThresholdConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return ThresholdConfig{}, err
	}
	return config, nil
}

func EvaluateGate(comparison Comparison, config ThresholdConfig) GateResult {
	if config.MaxSeverity == "" &&
		config.MinConfidence == "" &&
		!config.RequireCleanCohorts &&
		len(config.Metrics) == 0 &&
		!hasLeakThreshold(config.Leaks) &&
		!hasProblemThreshold(config.Problems) &&
		!config.AndroidComponents.Enabled {
		return GateResult{}
	}
	var failures []string
	for _, delta := range comparison.Deltas {
		if !delta.Comparable {
			continue
		}
		threshold := config.Metrics[delta.Name]
		maxSeverity := firstNonEmpty(threshold.MaxSeverity, config.MaxSeverity)
		if maxSeverity != "" && severityRank(delta.Severity) > severityRank(maxSeverity) {
			failures = append(failures, fmt.Sprintf("%s severity=%s exceeds %s", delta.Name, delta.Severity, maxSeverity))
		}
		if threshold.MaxRegressionPct > 0 && delta.RegressionPct > threshold.MaxRegressionPct {
			failures = append(failures, fmt.Sprintf("%s regression_pct=%.2f exceeds %.2f", delta.Name, delta.RegressionPct, threshold.MaxRegressionPct))
		}
		if threshold.MaxRegressionAbs > 0 && delta.RegressionAbs > threshold.MaxRegressionAbs {
			failures = append(failures, fmt.Sprintf("%s regression_abs=%.2f exceeds %.2f", delta.Name, delta.RegressionAbs, threshold.MaxRegressionAbs))
		}
	}
	if config.MinConfidence != "" && len(comparison.Deltas) > 0 {
		confidence := comparison.Deltas[0].Confidence
		if confidenceRank(confidence) < confidenceRank(config.MinConfidence) {
			failures = append(failures, fmt.Sprintf(
				"confidence=%s below %s (baseline logs/events=%d/%d, candidate logs/events=%d/%d; collection quality: %s, %s; collect 5+ logs and 500+ events per cohort and resolve collection-quality reasons for high confidence)",
				confidence,
				config.MinConfidence,
				comparison.Baseline.LogCount,
				comparison.Baseline.EventCount,
				comparison.Candidate.LogCount,
				comparison.Candidate.EventCount,
				collectionQualityGateDetail("baseline", comparison.Baseline),
				collectionQualityGateDetail("candidate", comparison.Candidate),
			))
		}
	}
	if config.RequireCleanCohorts && len(comparison.CohortWarnings) > 0 {
		for _, warning := range comparison.CohortWarnings {
			failures = append(failures, "cohort mismatch: "+warning)
		}
	}
	failures = append(failures, evaluateLeakGate(comparison, config.Leaks)...)
	failures = append(failures, evaluateProblemGate(comparison, config.Problems)...)
	failures = append(failures, evaluateAndroidComponentGate(comparison.AndroidComponents, config.AndroidComponents)...)
	return GateResult{Failed: len(failures) > 0, Failures: failures}
}

func evaluateAndroidComponentGate(comparison AndroidComponentComparison, config AndroidComponentGateThreshold) []string {
	if !config.Enabled {
		return nil
	}
	thresholds := androidComponentGateMetrics(config)
	if len(thresholds) == 0 {
		return []string{"android_components gate is enabled but no thresholds are configured"}
	}
	if failures := validateAndroidComponentGateMetrics(thresholds); len(failures) > 0 {
		return failures
	}
	if !comparison.Comparable {
		return []string{"android_components gate cannot evaluate incomparable process scopes or missing typed events: " + comparison.Note}
	}
	if comparison.Partial && !config.AllowPartial {
		return []string{"android_components gate rejects partial analysis; set allow_partial=true only for local lifecycle metrics: " + comparison.Note}
	}
	metrics := make(map[string]Delta, len(comparison.Metrics))
	for _, metric := range comparison.Metrics {
		metrics[metric.Name] = metric
	}
	failures := make([]string, 0, len(thresholds))
	for _, threshold := range thresholds {
		metric, ok := metrics[threshold.name]
		if !ok || !metric.Comparable {
			failures = append(failures, fmt.Sprintf("android_components %s cannot be evaluated from this comparison", threshold.name))
			continue
		}
		value := metric.RegressionAbs
		if threshold.relative {
			value = metric.RegressionPct
		}
		if threshold.minimum {
			value = metric.CandidateValue
			if value < threshold.limit {
				failures = append(failures, fmt.Sprintf("android_components %s candidate=%.2f below %.2f", threshold.name, value, threshold.limit))
			}
			continue
		}
		if value > threshold.limit {
			unit := "п.п."
			if threshold.relative {
				unit = "%"
			}
			failures = append(failures, fmt.Sprintf("android_components %s increase=%.2f %s exceeds %.2f", threshold.name, value, unit, threshold.limit))
		}
	}
	return failures
}

type androidComponentGateMetric struct {
	name     string
	limit    float64
	relative bool
	minimum  bool
}

func androidComponentGateMetrics(config AndroidComponentGateThreshold) []androidComponentGateMetric {
	metrics := make([]androidComponentGateMetric, 0, 11)
	appendLimit := func(name string, limit *float64, relative, minimum bool) {
		if limit != nil {
			metrics = append(metrics, androidComponentGateMetric{name: name, limit: *limit, relative: relative, minimum: minimum})
		}
	}
	appendLimit(androidMetricServiceFailureRate, config.MaxServiceFailureRateIncreasePP, false, false)
	appendLimit(androidMetricServiceTimeoutRate, config.MaxServiceTimeoutRateIncreasePP, false, false)
	appendLimit(androidMetricServiceSlowCallbackRate, config.MaxServiceSlowRateIncreasePP, false, false)
	appendLimit(androidMetricReceiverFailureRate, config.MaxReceiverFailureRateIncreasePP, false, false)
	appendLimit(androidMetricReceiverAsyncDeadlineRiskRate, config.MaxReceiverAsyncDeadlineRiskRateIncreasePP, false, false)
	appendLimit(androidMetricReceiverSyncSlowRate, config.MaxReceiverSyncSlowRateIncreasePP, false, false)
	appendLimit(androidMetricBinderClientP95, config.MaxBinderClientP95IncreasePct, true, false)
	appendLimit(androidMetricBinderSlowMainThreadRate, config.MaxBinderSlowMainThreadRateIncreasePP, false, false)
	appendLimit(androidMetricBinderFailureRate, config.MaxBinderFailureRateIncreasePP, false, false)
	appendLimit(androidMetricBinderUnhandledRate, config.MaxBinderUnhandledRateIncreasePP, false, false)
	appendLimit(androidMetricBinderCorrelationCoverage, config.MinBinderCorrelationCoveragePct, false, true)
	return metrics
}

func validateAndroidComponentGateMetrics(thresholds []androidComponentGateMetric) []string {
	var failures []string
	for _, threshold := range thresholds {
		if threshold.limit < 0 {
			failures = append(failures, fmt.Sprintf("android_components threshold for %s must not be negative", threshold.name))
		}
		if threshold.minimum && threshold.limit > 100 {
			failures = append(failures, fmt.Sprintf("android_components threshold for %s must be within 0..100", threshold.name))
		}
	}
	return failures
}

func evaluateProblemGate(comparison Comparison, config ProblemGateThreshold) []string {
	if !hasProblemThreshold(config) {
		return nil
	}
	excludedCategories := stringSet(config.ExcludeCategories)
	excludedDetectors := stringSet(config.ExcludeDetectors)
	minimumConfidence := firstNonEmpty(config.MinConfidence, "low")
	counts := map[string]int{}
	var failures []string
	for _, delta := range comparison.ProblemComparison.Deltas {
		finding := delta.Candidate
		if finding == nil {
			continue
		}
		if _, excluded := excludedCategories[finding.Category]; excluded {
			continue
		}
		if _, excluded := excludedDetectors[finding.DetectorID]; excluded {
			continue
		}
		counts[finding.Severity]++
		if problemConfidenceRank(finding.Confidence) < problemConfidenceRank(minimumConfidence) {
			continue
		}
		if config.MaxSeverity != "" && problemSeverityRank(finding.Severity) > problemSeverityRank(config.MaxSeverity) {
			failures = append(failures, fmt.Sprintf("problem %s severity=%s exceeds %s", finding.Fingerprint, finding.Severity, config.MaxSeverity))
		}
		if config.FailOnNew && delta.Status == "new" {
			failures = append(failures, fmt.Sprintf("new problem %s (%s)", finding.Fingerprint, finding.Title))
		}
		if config.FailOnRegressed && delta.Status == "regressed" {
			failures = append(failures, fmt.Sprintf("regressed problem %s (%s)", finding.Fingerprint, finding.Title))
		}
	}
	for _, limit := range []struct {
		severity string
		value    *int
	}{{"critical", config.MaxCritical}, {"high", config.MaxHigh}, {"medium", config.MaxMedium}} {
		if limit.value != nil && counts[limit.severity] > *limit.value {
			failures = append(failures, fmt.Sprintf("problems %s=%d exceeds %d", limit.severity, counts[limit.severity], *limit.value))
		}
	}
	coverage := map[string]CategoryCoverage{}
	for _, item := range comparison.Candidate.CategoryCoverage {
		coverage[item.Category] = item
	}
	for _, category := range config.RequiredCoverage {
		item, ok := coverage[category]
		if !ok || (item.Status != "healthy" && item.Status != "problems_found") {
			status := "missing"
			if ok {
				status = item.Status
			}
			failures = append(failures, fmt.Sprintf("required problem coverage %s is %s", category, status))
		}
	}
	return failures
}

func hasProblemThreshold(config ProblemGateThreshold) bool {
	return config.MaxCritical != nil || config.MaxHigh != nil || config.MaxMedium != nil || config.MaxSeverity != "" || config.MinConfidence != "" || config.FailOnNew || config.FailOnRegressed || len(config.ExcludeCategories) > 0 || len(config.ExcludeDetectors) > 0 || len(config.RequiredCoverage) > 0
}

func stringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		out[strings.TrimSpace(value)] = struct{}{}
	}
	return out
}

func collectionQualityGateDetail(label string, summary Summary) string {
	level := collectionConfidenceCap(summary)
	reasons := "no reported loss"
	if len(summary.CollectionQuality.Reasons) > 0 {
		reasons = strings.Join(summary.CollectionQuality.Reasons, "; ")
	}
	return fmt.Sprintf("%s=%s [%s]", label, level, reasons)
}

func evaluateLeakGate(comparison Comparison, config LeakThreshold) []string {
	if !hasLeakThreshold(config) {
		return nil
	}
	report := BuildLeakCompareReport(comparison)
	stats := report.Stats
	var failures []string
	if config.MaxCandidateTotal > 0 && stats.CandidateTotal > config.MaxCandidateTotal {
		failures = append(failures, fmt.Sprintf("leaks candidate_total=%d exceeds %d", stats.CandidateTotal, config.MaxCandidateTotal))
	}
	if config.MaxNew > 0 && stats.New > config.MaxNew {
		failures = append(failures, fmt.Sprintf("leaks new=%d exceeds %d", stats.New, config.MaxNew))
	}
	if config.MaxWorse > 0 && stats.Worse > config.MaxWorse {
		failures = append(failures, fmt.Sprintf("leaks worse=%d exceeds %d", stats.Worse, config.MaxWorse))
	}
	if config.FailOnNew && stats.New > 0 {
		failures = append(failures, fmt.Sprintf("leaks new=%d but fail_on_new=true", stats.New))
	}
	if config.FailOnWorse && stats.Worse > 0 {
		failures = append(failures, fmt.Sprintf("leaks worse=%d but fail_on_worse=true", stats.Worse))
	}
	if config.MaxHigh > 0 && report.Candidate.Stats.High > config.MaxHigh {
		failures = append(failures, fmt.Sprintf("leaks high=%d exceeds %d", report.Candidate.Stats.High, config.MaxHigh))
	}
	if config.MaxRuntimeOnly > 0 && report.Candidate.Stats.RuntimeOnly > config.MaxRuntimeOnly {
		failures = append(failures, fmt.Sprintf("leaks runtime_only=%d exceeds %d", report.Candidate.Stats.RuntimeOnly, config.MaxRuntimeOnly))
	}
	if config.FailOnNewHigh {
		for _, delta := range report.Deltas {
			if delta.Status == LeakDeltaNew && delta.Severity == "high" {
				failures = append(failures, "leaks new high severity: "+leakGateClass(delta))
			}
		}
	}
	if config.RequireHeapForHigh {
		for _, leak := range report.Candidate.Items {
			if leak.Suspect.Severity == "high" && !leak.Suspect.HeapEvidence {
				failures = append(failures, "leaks high severity without heap evidence: "+leak.Suspect.ClassName)
			}
		}
	}
	return failures
}

func hasLeakThreshold(config LeakThreshold) bool {
	return config.MaxCandidateTotal > 0 ||
		config.MaxNew > 0 ||
		config.MaxWorse > 0 ||
		config.MaxHigh > 0 ||
		config.MaxRuntimeOnly > 0 ||
		config.FailOnNew ||
		config.FailOnWorse ||
		config.FailOnNewHigh ||
		config.RequireHeapForHigh
}

func leakGateClass(delta LeakDelta) string {
	if delta.HasCandidate {
		return delta.Candidate.ClassName
	}
	return delta.Baseline.ClassName
}

func severityRank(value string) int {
	switch value {
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

func confidenceRank(value string) int {
	switch value {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}
