package analyze

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvidenceQualityKeepsProcessCoverageIndependentFromDiagnosticCompleteness(t *testing.T) {
	summary := evidenceQualityFixture()
	summary.CollectionQuality.DiagnosticCompletenessPercent = 100
	summary.CollectionQuality.ProcessRosterComplete = false
	summary.CollectionQuality.ExpectedProcessCount = 2
	summary.CollectionQuality.ObservedProcessCount = 1

	quality := BuildEvidenceQualityVector(summary)
	process := evidenceDimension(t, quality, "process_coverage")
	if process.Status != EvidenceQualityDegraded || !strings.Contains(process.Explanation, "1 из 2") {
		t.Fatalf("process quality = %+v, want explicit degraded 1/2 coverage", process)
	}
	if quality.Overall != EvidenceQualityDegraded {
		t.Fatalf("overall = %q, want degraded", quality.Overall)
	}
}

func TestEvidenceQualityRejectsCleanVerdictWhenCollectorIsMissing(t *testing.T) {
	summary := evidenceQualityFixture()
	summary.CategoryCoverage[0].Status = "not_measured"

	quality := BuildEvidenceQualityVector(summary)
	acquisition := evidenceDimension(t, quality, "acquisition")
	if acquisition.Status != EvidenceQualityInsufficient {
		t.Fatalf("acquisition = %+v, want insufficient", acquisition)
	}
	if quality.Overall != EvidenceQualityInsufficient || !strings.Contains(quality.Headline, "недостаточно для общего вывода") {
		t.Fatalf("quality = %+v, want explicit clean-verdict prohibition", quality)
	}
}

func TestEvidenceQualityDoesNotExposeProbabilityLookingScalar(t *testing.T) {
	quality := BuildEvidenceQualityVector(evidenceQualityFixture())
	encoded, err := json.Marshal(quality)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"trust_score", "confidence_percent", "probability"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("quality vector contains probability-looking scalar %q: %s", forbidden, encoded)
		}
	}
	estimator := evidenceDimension(t, quality, "estimator_calibration")
	if estimator.Status != EvidenceQualityNotCalibrated {
		t.Fatalf("estimator quality = %+v, want not calibrated", estimator)
	}
}

func TestEvidenceQualityUsesRussianProblemOrientedExplanations(t *testing.T) {
	summary := evidenceQualityFixture()
	summary.CollectionQuality.BoundedEvidenceLoss = 25661
	summary.CollectionQuality.ProcessRosterComplete = false
	summary.CollectionQuality.ExpectedProcessCount = 3
	summary.CollectionQuality.ObservedProcessCount = 1
	summary.AnalysisInputs = AnalysisInputCompleteness{
		Status:          "runtime_only",
		RuntimeEvidence: true,
		Explanation:     "доступны runtime evidence, но static graph отсутствует",
	}

	quality := BuildEvidenceQualityVector(summary)
	parts := []string{quality.Headline}
	for _, dimension := range quality.Dimensions {
		parts = append(parts, dimension.Label, dimension.Explanation)
	}
	visible := strings.ToLower(strings.Join(parts, " "))
	for _, forbidden := range []string{
		"evidence", "clean", "claim", "payload", "control", "committed", "chain",
		"collectors", "detector", "first-class", "configured scope", "run",
		"sealed", "terminal", "sensor", "frames", "source", "deadline",
		"distribution", "runtime", "build-time", "static graph",
	} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("quality explanation contains untranslated term %q: %s", forbidden, visible)
		}
	}
	if strings.Contains(visible, "25661") || !strings.Contains(visible, "часть диагностических данных") {
		t.Fatalf("quality explanation is not problem-oriented: %s", visible)
	}
}

func evidenceQualityFixture() Summary {
	return Summary{
		LogCount: 1,
		CollectionQuality: CollectionQuality{
			Complete:                true,
			ChainValid:              true,
			ExactAdmission:          true,
			CounterInvariantsValid:  true,
			QualityProgressionValid: true,
			ProcessRosterComplete:   true,
			ExpectedProcessCount:    1,
			ObservedProcessCount:    1,
			SealedSegments:          1,
			RunCohortConsistent:     true,
			AllProcessesConfigured:  true,
			ProcessScopeConsistent:  true,
			SegmentsWithQuality:     1,
			DecodedCommittedChunks:  1,
			ReportedCommittedChunks: 1,
		},
		CategoryCoverage: []CategoryCoverage{{Category: ProblemCategoryNetwork, Status: "healthy"}},
		AnalysisInputs: AnalysisInputCompleteness{
			Status:          "complete",
			Complete:        true,
			RuntimeEvidence: true,
		},
	}
}

func evidenceDimension(t *testing.T, quality EvidenceQualityVector, id string) EvidenceQualityDimension {
	t.Helper()
	for _, dimension := range quality.Dimensions {
		if dimension.ID == id {
			return dimension
		}
	}
	t.Fatalf("dimension %q not found in %+v", id, quality.Dimensions)
	return EvidenceQualityDimension{}
}
