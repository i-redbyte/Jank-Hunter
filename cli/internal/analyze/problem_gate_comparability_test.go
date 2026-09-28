package analyze

import (
	"strings"
	"testing"
)

func TestProblemGateRequiresComparableEvidenceForRelativeChecks(t *testing.T) {
	for _, status := range []string{"new", "regressed", "persistent", "not_comparable"} {
		for _, confidence := range []string{"low", "high"} {
			t.Run(status+"/"+confidence, func(t *testing.T) {
				finding := ProblemFinding{Fingerprint: "chat", Title: "Chat", Category: ProblemCategoryUI, DetectorID: "ui.test", Severity: "high", Confidence: confidence}
				delta := ProblemDelta{Fingerprint: "chat", Status: status, Candidate: &finding}
				result := EvaluateGate(Comparison{ProblemComparison: ProblemComparison{Deltas: []ProblemDelta{delta}}}, ThresholdConfig{Problems: ProblemGateThreshold{FailOnNew: true, FailOnRegressed: true, MinConfidence: "high"}})
				text := strings.Join(result.Failures, ";")
				if !result.Failed || !strings.Contains(text, "cannot be evaluated") || strings.Contains(text, "new problem ") || strings.Contains(text, "regressed problem ") {
					t.Fatalf("incomparable input mislabeled: %+v", result)
				}
			})
		}
	}
}

func TestProblemGateKeepsAbsoluteChecksAndExplicitExclusions(t *testing.T) {
	finding := ProblemFinding{Fingerprint: "chat", Category: ProblemCategoryUI, DetectorID: "ui.test", Severity: "high", Confidence: "high"}
	comparison := Comparison{ProblemComparison: ProblemComparison{Deltas: []ProblemDelta{{Fingerprint: "chat", Status: "new", Candidate: &finding}}}}
	absolute := EvaluateGate(comparison, ThresholdConfig{Problems: ProblemGateThreshold{MaxSeverity: "medium"}})
	if !absolute.Failed || strings.Contains(strings.Join(absolute.Failures, ";"), "cannot be evaluated") {
		t.Fatalf("absolute check confused with regression: %+v", absolute)
	}
	for _, config := range []ProblemGateThreshold{
		{FailOnNew: true, ExcludeCategories: []string{ProblemCategoryUI}},
		{FailOnRegressed: true, ExcludeDetectors: []string{"ui.test"}},
	} {
		if got := EvaluateGate(comparison, ThresholdConfig{Problems: config}); got.Failed {
			t.Fatalf("excluded scope was evaluated: %+v", got)
		}
	}
	comparison.ProblemComparison.Deltas[0].Comparable = true
	if got := EvaluateGate(comparison, ThresholdConfig{Problems: ProblemGateThreshold{FailOnNew: true}}); !got.Failed || !strings.Contains(strings.Join(got.Failures, ";"), "new problem") {
		t.Fatalf("comparable new finding lost: %+v", got)
	}
}
