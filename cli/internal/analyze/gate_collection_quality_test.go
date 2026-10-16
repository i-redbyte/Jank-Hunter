package analyze

import (
	"strings"
	"testing"
)

func TestCompletenessGateRejectsDifferentMeasurementModels(t *testing.T) {
	before := Summary{CollectionQuality: CollectionQuality{DiagnosticCompletenessPercent: 100, DiagnosticCompletenessModel: "model-v1"}}
	after := Summary{CollectionQuality: CollectionQuality{DiagnosticCompletenessPercent: 100, DiagnosticCompletenessModel: "model-v2"}}
	result := EvaluateGate(Compare(before, after), ThresholdConfig{Metrics: map[string]MetricThreshold{
		completenessMetric: {MaxRegressionAbs: floatPointer(5)},
	}})
	if !result.Failed || !strings.Contains(strings.Join(result.Failures, " "), "model") {
		t.Fatalf("incompatible percentages passed quality gate: %+v", result)
	}
}
