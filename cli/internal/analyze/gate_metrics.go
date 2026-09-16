package analyze

import (
	"fmt"
	"math"
	"sort"
)

func hasGateThreshold(config ThresholdConfig) bool {
	return config.MaxSeverity != "" || config.MinConfidence != "" || config.RequireCleanCohorts ||
		len(config.Metrics) > 0 || hasLeakThreshold(config.Leaks) || hasProblemThreshold(config.Problems) || config.AndroidComponents.Enabled
}

// Names are the stable comparison/export keys. Category mixes support severity only;
// their Delta numeric fields do not describe a measured regression.
func gateMetricKind(name string) (known, numeric bool) {
	switch name {
	case "HTTP p95", "HTTP failure rate", "UI jank rate", "UI avg FPS", stallDurationMetric,
		"Max PSS", "Min available memory", "UID RX delta", "UID TX delta", "Retained objects",
		"Log spam", "Problem windows", completenessMetric:
		return true, true
	case "Process mix", "App version mix", "SDK mix", "Device mix", "Network mix", "Cohort mix":
		return true, false
	default:
		return false, false
	}
}

func sortedGateMetricNames(metrics map[string]MetricThreshold) []string {
	names := make([]string, 0, len(metrics))
	for name := range metrics {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func validateGateMetric(name string, threshold MetricThreshold, globalSeverity string) error {
	for _, limit := range []*float64{threshold.MaxRegressionAbs, threshold.MaxRegressionPct} {
		if limit != nil && *limit < 0 {
			return fmt.Errorf("metrics.%s regression thresholds must not be negative", name)
		}
		if limit != nil && !finiteGateValue(*limit) {
			return fmt.Errorf("metrics.%s regression thresholds must be finite", name)
		}
	}
	known, numeric := gateMetricKind(name)
	if !known {
		return fmt.Errorf("metrics contains unknown metric %q", name)
	}
	if !numeric && (threshold.MaxRegressionAbs != nil || threshold.MaxRegressionPct != nil) {
		return fmt.Errorf("metrics.%s supports max_severity only; numeric regression is unavailable", name)
	}
	if threshold.MaxSeverity == "" && globalSeverity == "" && threshold.MaxRegressionAbs == nil && threshold.MaxRegressionPct == nil {
		return fmt.Errorf("metrics.%s has no configured constraint", name)
	}
	return nil
}

func finiteGateValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func evaluateMetricGate(comparison Comparison, config ThresholdConfig) []string {
	var failures []string
	seen := make(map[string]bool, len(config.Metrics))
	for _, change := range comparison.Deltas {
		threshold, requested := config.Metrics[change.Name]
		if requested {
			seen[change.Name] = true
		}
		if !requested && config.MaxSeverity == "" {
			continue
		}
		failures = append(failures, evaluateMetricConstraint(change, threshold, config.MaxSeverity)...)
	}
	for _, name := range sortedGateMetricNames(config.Metrics) {
		if name != completenessMetric && !seen[name] {
			failures = append(failures, name+" cannot be evaluated: metric is absent from the comparison")
		}
	}
	if config.MaxSeverity != "" && len(comparison.Deltas) == 0 {
		failures = append(failures, "max_severity cannot be evaluated: comparison has no metrics")
	}
	return failures
}

func evaluateMetricConstraint(change Delta, threshold MetricThreshold, globalSeverity string) []string {
	if !change.Comparable {
		return []string{change.Name + " cannot be evaluated: " + change.ComparisonNote}
	}
	if !finiteGateValue(change.BaselineValue) || !finiteGateValue(change.CandidateValue) ||
		!finiteGateValue(change.RegressionAbs) || !finiteGateValue(change.RegressionPct) {
		return []string{change.Name + " cannot be evaluated: non-finite metric value"}
	}
	var failures []string
	maxSeverity := firstNonEmpty(threshold.MaxSeverity, globalSeverity)
	if maxSeverity != "" && severityRank(change.Severity) > severityRank(maxSeverity) {
		failures = append(failures, fmt.Sprintf("%s severity=%s exceeds %s", change.Name, change.Severity, maxSeverity))
	}
	if threshold.MaxRegressionAbs != nil && change.RegressionAbs > *threshold.MaxRegressionAbs {
		failures = append(failures, fmt.Sprintf("%s regression_abs=%.2f exceeds %.2f", change.Name, change.RegressionAbs, *threshold.MaxRegressionAbs))
	}
	if threshold.MaxRegressionPct != nil {
		if change.BaselineValue == 0 && change.RegressionAbs > 0 {
			failures = append(failures, change.Name+" cannot be evaluated as a percentage: zero baseline; configure max_regression_abs")
		} else if change.RegressionPct > *threshold.MaxRegressionPct {
			failures = append(failures, fmt.Sprintf("%s regression_pct=%.2f exceeds %.2f", change.Name, change.RegressionPct, *threshold.MaxRegressionPct))
		}
	}
	return failures
}
