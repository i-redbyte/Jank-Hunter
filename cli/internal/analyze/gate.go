package analyze

import (
	"encoding/json"
	"fmt"
	"os"
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
	if config.MaxSeverity == "" && config.MinConfidence == "" && len(config.Metrics) == 0 {
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
			failures = append(failures, fmt.Sprintf("confidence=%s below %s", confidence, config.MinConfidence))
		}
	}
	return GateResult{Failed: len(failures) > 0, Failures: failures}
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
