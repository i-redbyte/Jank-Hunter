package analyze

import (
	"fmt"
	"strings"
)

const maxThresholdConfigBytes = 1 << 20

func LoadThresholdConfig(path string) (ThresholdConfig, error) {
	if path == "" {
		return ThresholdConfig{}, nil
	}
	data, err := readBoundedFile(path, "threshold config", maxThresholdConfigBytes)
	if err != nil {
		return ThresholdConfig{}, err
	}
	var config ThresholdConfig
	if err := decodeStrictJSON(data, &config); err != nil {
		return ThresholdConfig{}, err
	}
	if err := validateThresholdConfig(config); err != nil {
		return ThresholdConfig{}, err
	}
	if !hasGateThreshold(config) {
		return ThresholdConfig{}, fmt.Errorf("threshold config contains no enabled checks; omit --thresholds to disable the gate")
	}
	return config, nil
}

func validateThresholdConfig(config ThresholdConfig) error {
	if !validGateSeverity(config.MaxSeverity, true) {
		return fmt.Errorf("max_severity %q must be one of ok, low, medium, high, critical", config.MaxSeverity)
	}
	if !validGateConfidence(config.MinConfidence) {
		return fmt.Errorf("min_confidence %q must be one of low, medium, high", config.MinConfidence)
	}
	for _, name := range sortedGateMetricNames(config.Metrics) {
		threshold := config.Metrics[name]
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("metrics contains an empty metric name")
		}
		if !validGateSeverity(threshold.MaxSeverity, true) {
			return fmt.Errorf("metrics.%s.max_severity %q is invalid", name, threshold.MaxSeverity)
		}
		if err := validateGateMetric(name, threshold, config.MaxSeverity); err != nil {
			return err
		}
	}
	for _, threshold := range []struct {
		name  string
		value *int
	}{
		{"max_candidate_total", config.Leaks.MaxCandidateTotal},
		{"max_new", config.Leaks.MaxNew},
		{"max_worse", config.Leaks.MaxWorse},
		{"max_high", config.Leaks.MaxHigh},
		{"max_runtime_only", config.Leaks.MaxRuntimeOnly},
	} {
		if threshold.value != nil && *threshold.value < 0 {
			return fmt.Errorf("leaks.%s must not be negative", threshold.name)
		}
	}
	if !validGateSeverity(config.Problems.MaxSeverity, false) {
		return fmt.Errorf("problems.max_severity %q must be one of info, low, medium, high, critical", config.Problems.MaxSeverity)
	}
	if !validGateConfidence(config.Problems.MinConfidence) {
		return fmt.Errorf("problems.min_confidence %q must be one of low, medium, high", config.Problems.MinConfidence)
	}
	for _, threshold := range []struct {
		name  string
		value *int
	}{
		{"max_critical", config.Problems.MaxCritical},
		{"max_high", config.Problems.MaxHigh},
		{"max_medium", config.Problems.MaxMedium},
	} {
		if threshold.value != nil && *threshold.value < 0 {
			return fmt.Errorf("problems.%s must not be negative", threshold.name)
		}
	}
	if failures := validateAndroidComponentGateMetrics(androidComponentGateMetrics(config.AndroidComponents)); len(failures) > 0 {
		return fmt.Errorf("invalid android_components thresholds: %s", strings.Join(failures, "; "))
	}
	return nil
}

func validGateSeverity(value string, generic bool) bool {
	if value == "" || value == "low" || value == "medium" || value == "high" || value == "critical" {
		return true
	}
	if generic {
		return value == "ok"
	}
	return value == "info"
}

func validGateConfidence(value string) bool {
	return value == "" || value == "low" || value == "medium" || value == "high"
}

func EvaluateGate(comparison Comparison, config ThresholdConfig) GateResult {
	if err := validateThresholdConfig(config); err != nil {
		return failedGateResult(err.Error())
	}
	if !hasGateThreshold(config) {
		return GateResult{Status: GateDisabled}
	}
	failures := evaluateCompletenessGate(comparison, config)
	var inconclusive []string
	blockedReason := scenarioGateBlockedReason(comparison.Scope.Comparability, config)
	if blockedReason == "" {
		failures = append(failures, evaluateMetricGate(comparison, config)...)
	} else {
		inconclusive = append(inconclusive, blockedReason)
		inconclusive = append(inconclusive, evaluateMetricGate(comparison, config)...)
	}
	if config.MinConfidence != "" && len(comparison.Deltas) == 0 {
		if blockedReason == "" {
			failures = append(failures, "min_confidence cannot be evaluated: comparison has no metrics")
		} else {
			inconclusive = append(inconclusive, "min_confidence cannot be evaluated: comparison has no metrics")
		}
	}
	if config.MinConfidence != "" && len(comparison.Deltas) > 0 {
		confidence := comparison.Deltas[0].Confidence
		if confidenceRank(confidence) < confidenceRank(config.MinConfidence) {
			message := fmt.Sprintf(
				"confidence=%s below %s (%s; %s; collection quality: %s, %s; collect 5+ independent acquisition groups and 500+ non-session events per cohort and resolve collection-quality reasons for high confidence)",
				confidence,
				config.MinConfidence,
				acquisitionGateDetail("baseline", comparison.Baseline),
				acquisitionGateDetail("candidate", comparison.Candidate),
				collectionQualityGateDetail("baseline", comparison.Baseline),
				collectionQualityGateDetail("candidate", comparison.Candidate),
			)
			if blockedReason == "" {
				failures = append(failures, message)
			} else {
				inconclusive = append(inconclusive, message)
			}
		}
	}
	if config.RequireCleanCohorts && len(comparison.CohortWarnings) > 0 {
		for _, warning := range comparison.CohortWarnings {
			message := "cohort mismatch: " + warning
			if blockedReason == "" {
				failures = append(failures, message)
			} else {
				inconclusive = append(inconclusive, message)
			}
		}
	}
	if blockedReason == "" {
		failures = append(failures, evaluateLeakGate(comparison, config.Leaks)...)
		failures = append(failures, evaluateProblemGate(comparison, config.Problems)...)
	} else {
		failures = append(failures, evaluateLeakGate(comparison, absoluteLeakThreshold(config.Leaks))...)
		failures = append(failures, evaluateProblemGate(comparison, absoluteProblemThreshold(config.Problems))...)
		inconclusive = append(inconclusive, evaluateLeakGate(comparison, relativeLeakThreshold(config.Leaks))...)
		inconclusive = append(inconclusive, evaluateProblemGate(comparison, relativeProblemThreshold(config.Problems))...)
	}
	failures = append(failures, evaluateAndroidComponentGate(comparison.AndroidComponents, config.AndroidComponents)...)
	return gateResultFromFailures(failures, inconclusive)
}

func scenarioGateBlockedReason(comparability ScenarioComparability, config ThresholdConfig) string {
	if !hasScenarioRelativeThreshold(config) {
		return ""
	}
	switch comparability {
	case ScenarioFull:
		return ""
	case ScenarioPartial:
		if config.AllowPartialComparison {
			return ""
		}
		return "regression gate cannot evaluate partial scenario coverage without allow_partial_comparison=true"
	case ScenarioNone:
		return "regression gate cannot evaluate reports without comparable scenarios"
	case ScenarioUnknown:
		return "regression gate cannot evaluate reports with unknown scenario coverage"
	default:
		// Hand-built legacy comparisons have no scenario field. Their individual
		// Comparable flags remain authoritative until they are migrated to v2.
		return ""
	}
}

func hasScenarioRelativeThreshold(config ThresholdConfig) bool {
	metricChecks := config.MaxSeverity != "" || config.MinConfidence != "" || config.RequireCleanCohorts
	for name := range config.Metrics {
		if name != completenessMetric {
			metricChecks = true
			break
		}
	}
	leaks := config.Leaks
	problems := config.Problems
	return metricChecks || leaks.MaxNew != nil || leaks.MaxWorse != nil || leaks.FailOnNew || leaks.FailOnWorse || leaks.FailOnNewHigh ||
		problems.FailOnNew || problems.FailOnRegressed
}

func absoluteLeakThreshold(config LeakThreshold) LeakThreshold {
	return LeakThreshold{
		MaxCandidateTotal: config.MaxCandidateTotal,
		MaxHigh:           config.MaxHigh, MaxRuntimeOnly: config.MaxRuntimeOnly,
		RequireHeapForHigh: config.RequireHeapForHigh,
	}
}

func relativeLeakThreshold(config LeakThreshold) LeakThreshold {
	return LeakThreshold{
		MaxNew: config.MaxNew, MaxWorse: config.MaxWorse,
		FailOnNew: config.FailOnNew, FailOnWorse: config.FailOnWorse, FailOnNewHigh: config.FailOnNewHigh,
	}
}

func absoluteProblemThreshold(config ProblemGateThreshold) ProblemGateThreshold {
	return ProblemGateThreshold{
		MaxCritical: config.MaxCritical, MaxHigh: config.MaxHigh, MaxMedium: config.MaxMedium,
		MaxSeverity: config.MaxSeverity, MinConfidence: config.MinConfidence,
		ExcludeCategories: config.ExcludeCategories, ExcludeDetectors: config.ExcludeDetectors,
		RequiredCoverage: config.RequiredCoverage,
	}
}

func relativeProblemThreshold(config ProblemGateThreshold) ProblemGateThreshold {
	return ProblemGateThreshold{
		MinConfidence: config.MinConfidence, FailOnNew: config.FailOnNew, FailOnRegressed: config.FailOnRegressed,
		ExcludeCategories: config.ExcludeCategories, ExcludeDetectors: config.ExcludeDetectors,
	}
}

func failedGateResult(message string) GateResult {
	return GateResult{Status: GateFail, Failed: true, Failures: []string{message}, Checks: []GateCheck{{Status: GateFail, Message: message}}}
}

func gateResultFromFailures(failures, inconclusive []string) GateResult {
	if len(failures) == 0 && len(inconclusive) == 0 {
		return GateResult{Status: GatePass, Checks: []GateCheck{{Status: GatePass}}}
	}
	all := make([]string, 0, len(failures)+len(inconclusive))
	all = append(all, failures...)
	all = append(all, inconclusive...)
	result := GateResult{Status: GateInconclusive, Failed: true, Failures: all, Checks: make([]GateCheck, 0, len(all))}
	for _, failure := range failures {
		status := GateFail
		if gateFailureIsInconclusive(failure) {
			status = GateInconclusive
		}
		result.Checks = append(result.Checks, GateCheck{Status: status, Message: failure})
		if status == GateFail {
			result.Status = GateFail
		}
	}
	for _, message := range inconclusive {
		result.Checks = append(result.Checks, GateCheck{Status: GateInconclusive, Message: message})
	}
	return result
}

func gateFailureIsInconclusive(message string) bool {
	return strings.Contains(message, "cannot be evaluated") ||
		strings.Contains(message, "cannot evaluate") ||
		strings.Contains(message, "incomparable") ||
		strings.Contains(message, "not comparable") ||
		strings.Contains(message, "confidence=") && strings.Contains(message, " below ") ||
		strings.HasPrefix(message, "cohort mismatch:") ||
		strings.Contains(message, "partial scenario coverage") ||
		strings.Contains(message, "partial analysis")
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
		if !finiteGateValue(metric.BaselineValue) || !finiteGateValue(metric.CandidateValue) ||
			!finiteGateValue(metric.RegressionAbs) || !finiteGateValue(metric.RegressionPct) {
			failures = append(failures, fmt.Sprintf("android_components %s cannot be evaluated: non-finite metric value", threshold.name))
			continue
		}
		if threshold.relative && metric.BaselineValue == 0 && metric.RegressionAbs > 0 {
			failures = append(failures, fmt.Sprintf("android_components %s cannot be evaluated as a percentage: zero baseline", threshold.name))
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
		if !finiteGateValue(threshold.limit) {
			failures = append(failures, fmt.Sprintf("android_components threshold for %s must be finite", threshold.name))
		}
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
			finding = delta.Baseline
		}
		if finding == nil {
			continue
		}
		if _, excluded := excludedCategories[finding.Category]; excluded {
			continue
		}
		if _, excluded := excludedDetectors[finding.DetectorID]; excluded {
			continue
		}
		// A detector's confidence does not establish before/after comparability.
		// Check this before confidence filtering and before skipping baseline-only
		// findings; neither absence nor weak evidence can turn a requested check green.
		newCheckUnavailable := config.FailOnNew && delta.Candidate != nil && delta.Baseline == nil && !delta.Comparable
		regressionCheckUnavailable := config.FailOnRegressed && delta.Candidate != nil && delta.Baseline != nil && !delta.Comparable
		if newCheckUnavailable || regressionCheckUnavailable {
			failures = append(failures, fmt.Sprintf("problem comparison %s cannot be evaluated: observations are not comparable", delta.Fingerprint))
		}
		if delta.Candidate == nil {
			continue
		}
		counts[finding.Severity]++
		if problemConfidenceRank(finding.Confidence) < problemConfidenceRank(minimumConfidence) {
			continue
		}
		if config.MaxSeverity != "" && problemSeverityRank(finding.Severity) > problemSeverityRank(config.MaxSeverity) {
			failures = append(failures, fmt.Sprintf("problem %s severity=%s exceeds %s", finding.Fingerprint, finding.Severity, config.MaxSeverity))
		}
		if config.FailOnNew && delta.Comparable && (delta.Status == "new" || delta.Observation == ObservationObservedOnlyAfter) {
			failures = append(failures, fmt.Sprintf("new problem %s (%s)", finding.Fingerprint, finding.Title))
		}
		if config.FailOnRegressed && delta.Comparable && delta.Status == "regressed" {
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
	if config.MaxCandidateTotal != nil && stats.CandidateTotal > *config.MaxCandidateTotal {
		failures = append(failures, fmt.Sprintf("leaks candidate_total=%d exceeds %d", stats.CandidateTotal, *config.MaxCandidateTotal))
	}
	if config.MaxNew != nil && stats.New > *config.MaxNew {
		failures = append(failures, fmt.Sprintf("leaks new=%d exceeds %d", stats.New, *config.MaxNew))
	}
	if config.MaxWorse != nil && stats.Worse > *config.MaxWorse {
		failures = append(failures, fmt.Sprintf("leaks worse=%d exceeds %d", stats.Worse, *config.MaxWorse))
	}
	if config.FailOnNew && stats.New > 0 {
		failures = append(failures, fmt.Sprintf("leaks new=%d but fail_on_new=true", stats.New))
	}
	if config.FailOnWorse && stats.Worse > 0 {
		failures = append(failures, fmt.Sprintf("leaks worse=%d but fail_on_worse=true", stats.Worse))
	}
	if config.MaxHigh != nil && report.Candidate.Stats.High > *config.MaxHigh {
		failures = append(failures, fmt.Sprintf("leaks high=%d exceeds %d", report.Candidate.Stats.High, *config.MaxHigh))
	}
	if config.MaxRuntimeOnly != nil && report.Candidate.Stats.RuntimeOnly > *config.MaxRuntimeOnly {
		failures = append(failures, fmt.Sprintf("leaks runtime_only=%d exceeds %d", report.Candidate.Stats.RuntimeOnly, *config.MaxRuntimeOnly))
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
	return config.MaxCandidateTotal != nil ||
		config.MaxNew != nil ||
		config.MaxWorse != nil ||
		config.MaxHigh != nil ||
		config.MaxRuntimeOnly != nil ||
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
