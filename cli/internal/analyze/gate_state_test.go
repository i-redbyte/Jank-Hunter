package analyze

import (
	"strings"
	"testing"
)

func TestGateFiniteStateContract(t *testing.T) {
	regression := Delta{
		Name:           "HTTP p95",
		Comparable:     true,
		BaselineValue:  100,
		CandidateValue: 120,
		RegressionAbs:  20,
		RegressionPct:  20,
		Severity:       "medium",
	}
	limit := floatPointer(25)
	config := ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionAbs: limit}}}

	tests := []struct {
		name       string
		comparison Comparison
		config     ThresholdConfig
		want       GateStatus
		failed     bool
	}{
		{name: "disabled", want: GateDisabled},
		{name: "pass", comparison: Comparison{Deltas: []Delta{regression}}, config: config, want: GatePass},
		{name: "fail", comparison: Comparison{Deltas: []Delta{regression}}, config: ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionAbs: floatPointer(10)}}}, want: GateFail, failed: true},
		{name: "inconclusive", config: config, want: GateInconclusive, failed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := EvaluateGate(test.comparison, test.config)
			if result.Status != test.want || result.Failed != test.failed {
				t.Fatalf("gate result = %+v, want status=%s failed=%t", result, test.want, test.failed)
			}
		})
	}
}

func TestGateClassifiesUnavailableProblemComparisonAsInconclusive(t *testing.T) {
	finding := ProblemFinding{Fingerprint: "chat", Category: ProblemCategoryUI, DetectorID: "ui.test", Severity: "high", Confidence: "high"}
	comparison := Comparison{ProblemComparison: ProblemComparison{Deltas: []ProblemDelta{{Fingerprint: "chat", Candidate: &finding}}}}
	result := EvaluateGate(comparison, ThresholdConfig{Problems: ProblemGateThreshold{FailOnNew: true}})
	if result.Status != GateInconclusive || !result.Failed {
		t.Fatalf("unavailable relative problem check = %+v", result)
	}
	if len(result.Checks) != 1 || result.Checks[0].Status != GateInconclusive {
		t.Fatalf("machine-readable checks = %+v", result.Checks)
	}
}

func TestGateRequiresExplicitPartialScenarioPolicy(t *testing.T) {
	comparison := Comparison{
		Scope:  ComparisonScope{Comparability: ScenarioPartial},
		Deltas: []Delta{{Name: "HTTP p95", Comparable: true, BaselineValue: 100, CandidateValue: 100}},
	}
	config := ThresholdConfig{Metrics: map[string]MetricThreshold{"HTTP p95": {MaxRegressionAbs: floatPointer(0)}}}
	result := EvaluateGate(comparison, config)
	if result.Status != GateInconclusive || !strings.Contains(strings.Join(result.Failures, ";"), "partial") {
		t.Fatalf("implicit partial gate = %+v", result)
	}

	config.AllowPartialComparison = true
	result = EvaluateGate(comparison, config)
	if result.Status != GatePass || result.Failed {
		t.Fatalf("explicit partial gate = %+v", result)
	}
}

func TestGateKeepsAbsoluteCandidateChecksOutsideScenarioComparison(t *testing.T) {
	finding := ProblemFinding{Fingerprint: "chat", Category: ProblemCategoryUI, DetectorID: "ui.test", Severity: "high", Confidence: "high"}
	comparison := Comparison{
		Scope:             ComparisonScope{Comparability: ScenarioNone},
		ProblemComparison: ProblemComparison{Deltas: []ProblemDelta{{Fingerprint: "chat", Candidate: &finding}}},
	}
	result := EvaluateGate(comparison, ThresholdConfig{Problems: ProblemGateThreshold{MaxSeverity: "medium"}})
	if result.Status != GateFail || !result.Failed {
		t.Fatalf("absolute candidate check = %+v", result)
	}
}

func TestGateInvalidConfigurationIsFailure(t *testing.T) {
	result := EvaluateGate(Comparison{}, ThresholdConfig{MaxSeverity: "unknown"})
	if result.Status != GateFail || !result.Failed || len(result.Checks) != 1 {
		t.Fatalf("invalid config = %+v", result)
	}
}
