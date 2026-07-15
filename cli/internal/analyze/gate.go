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
		!hasLeakThreshold(config.Leaks) {
		return GateResult{}
	}
	var failures []string
	for _, delta := range comparison.Deltas {
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
	return GateResult{Failed: len(failures) > 0, Failures: failures}
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
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
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
