package analyze

const completenessMetric = "diagnostic_completeness_percent"

// An unknown buffer volume cannot pass an explicitly requested data-quality check.
func evaluateCompletenessGate(comparison Comparison, config ThresholdConfig) []string {
	threshold, requested := config.Metrics[completenessMetric]
	before, after := comparison.Baseline.CollectionQuality, comparison.Candidate.CollectionQuality
	unknown := before.DiagnosticCompletenessPercent < 0 || after.DiagnosticCompletenessPercent < 0
	if unknown {
		if requested || config.MinConfidence != "" || config.RequireCleanCohorts {
			return []string{completenessMetric + " cannot be evaluated: полнота неизвестна"}
		}
		return nil
	}
	if !requested {
		return nil
	}
	if before.DiagnosticCompletenessModel == "" || after.DiagnosticCompletenessModel == "" {
		return []string{completenessMetric + " cannot be evaluated: completeness model is unavailable"}
	}
	change := deltaFloat(completenessMetric, before.DiagnosticCompletenessPercent, after.DiagnosticCompletenessPercent, "%", false, 1)
	return evaluateMetricConstraint(change, threshold, config.MaxSeverity)
}
